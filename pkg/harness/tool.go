package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/vince-0202/acgo/pkg/communi"
	"github.com/vince-0202/acgo/pkg/errors"
	"github.com/vince-0202/acgo/pkg/keys"
	"github.com/vince-0202/acgo/pkg/llm"
	"github.com/xeipuuv/gojsonschema"
	"strings"
)

// Tool is the interface implemented by all tools usable by the Agent.
type Tool interface {
	Name() string
	Label() string
	Description() string
	JSONSchema() map[string]any
	Execute(ctx context.Context, toolCallID string, args json.RawMessage, update ToolUpdateFunc) communi.ToolCallResult
}

func NewToolController(agentId string, cc *ContextController, emf func(e communi.AgentEvent), tools ...Tool) *ToolController {
	toolMap := make(map[string]Tool)
	for _, tool := range tools {
		toolMap[tool.Name()] = tool
	}
	return &ToolController{
		agentId:           agentId,
		tools:             toolMap,
		contextController: cc,
		pendingToolCalls:  make([]communi.ToolCallRequest, 0),
		emitFunc:          emf,
	}
}

type ToolController struct {
	agentId           string
	tools             map[string]Tool
	contextController *ContextController
	pendingToolCalls  []communi.ToolCallRequest
	emitFunc          func(e communi.AgentEvent)
}

func (tc *ToolController) Load() {

}

func (tc *ToolController) CleanPendingTool() {
	tc.pendingToolCalls = make([]communi.ToolCallRequest, 0)
}

func (tc *ToolController) Execute(ctx context.Context) {
	if tc == nil || tc.contextController == nil {
		return
	}
	if tc.pendingToolCalls == nil {
		return
	}
	calls := tc.pendingToolCalls
	tc.pendingToolCalls = nil

	type slot struct {
		call   communi.ToolCallRequest
		args   json.RawMessage
		tool   Tool
		phase  string // "unknown", "val_err", "ok"
		result communi.ToolCallResult
	}
	slots := make([]slot, len(calls))

	for i, call := range calls {
		args := normalizeToolCallArguments(call.Arguments)
		tool, ok := tc.FindTool(call.Name)
		if !ok {
			toolMsg := communi.NewToolCallErrorMessage(
				call.ID, ErrUnknownProvider(call.Name),
			)
			tc.contextController.AppendMessage(toolMsg)
			tc.emitFunc(communi.AgentEvent{
				Type:       communi.EventToolExecutionEnd,
				AgentID:    tc.agentId,
				ToolName:   call.Name,
				ToolCallID: call.ID,
				ToolArgs:   args,
				Message:    &toolMsg,
				Error:      ErrUnknownProvider(call.Name),
				ErrorKind:  errors.ErrKindTool,
			})
			slots[i].phase = "unknown"
			continue
		}
		args = CoerceToolArguments(tool.JSONSchema(), args)
		tc.emitFunc(communi.AgentEvent{
			Type:       communi.EventToolExecutionStart,
			AgentID:    tc.agentId,
			ToolName:   tool.Name(),
			ToolCallID: call.ID,
			ToolArgs:   args,
		})

		if err := ValidateToolArguments(tool.Name(), tool.JSONSchema(), args); err != nil {
			toolMsg := communi.NewToolCallErrorMessage(call.ID, err)
			tc.contextController.AppendMessage(toolMsg)
			tc.emitFunc(communi.AgentEvent{
				Type:       communi.EventToolExecutionEnd,
				AgentID:    tc.agentId,
				ToolName:   tool.Name(),
				ToolCallID: call.ID,
				ToolArgs:   args,
				Message:    &toolMsg,
				Error:      err,
				ErrorKind:  errors.ErrKindTool,
			})
			slots[i].phase = "val_err"
			continue
		}

		slots[i] = slot{call: call, args: args, tool: tool, phase: "ok"}
	}

	for i := range slots {
		if slots[i].phase != "ok" {
			continue
		}
		call := slots[i].call
		args := slots[i].args
		tool := slots[i].tool
		result := tool.Execute(ctx, call.ID, args, nil)
		slots[i].result = result
	}

	for i := range slots {
		if slots[i].phase != "ok" {
			continue
		}
		call := slots[i].call
		args := slots[i].args
		tool := slots[i].tool
		result := slots[i].result
		toolMsg, errVal := toolResultToMessage(call.ID, result)
		tc.contextController.AppendMessage(toolMsg)
		tc.emitFunc(communi.AgentEvent{
			Type:       communi.EventToolExecutionEnd,
			AgentID:    tc.agentId,
			ToolName:   tool.Name(),
			ToolCallID: call.ID,
			ToolArgs:   args,
			Message:    &toolMsg,
			Error:      errVal,
			ErrorKind:  errors.ErrKindTool,
		})
	}
}

func toolResultToMessage(toolCallID string, result communi.ToolCallResult) (communi.Message, error) {
	if result.IsError() {
		toolMsg := communi.NewToolCallErrorMessage(toolCallID, result.Error)
		return toolMsg, result.Error
	}
	toolMsg := communi.Message{
		ID:      "tool-" + toolCallID,
		Role:    keys.AgentRoleTool,
		Content: result.Content,
		ToolCall: &communi.ToolCallRequest{
			ID: toolCallID,
		},
		IsError:  result.IsError(),
		Metadata: result.Metadata,
	}
	return toolMsg, nil
}

func (tc *ToolController) GetPendingToolCalls() []communi.ToolCallRequest {
	return tc.pendingToolCalls
}

func (tc *ToolController) FindTool(name string) (tool Tool, ok bool) {
	tool, ok = tc.tools[name]
	return
}

// UpsertPendingToolCall tracks the latest version of a ToolCall by ID.
// Stored Arguments are always normalized so they are non-nil and usable for execution.
func (tc *ToolController) UpsertPendingToolCall(call communi.ToolCallRequest) {
	normalized := communi.ToolCallRequest{
		ID:        call.ID,
		Name:      call.Name,
		Arguments: llm.NormalizeToolCallArguments(call.Arguments),
	}
	for i := range tc.pendingToolCalls {
		if tc.pendingToolCalls[i].ID == call.ID {
			tc.pendingToolCalls[i] = normalized
			return
		}
	}
	tc.pendingToolCalls = append(tc.pendingToolCalls, normalized)
}

func (tc *ToolController) GetToolSchemas() []communi.ToolSchema {
	if len(tc.tools) == 0 {
		return nil
	}
	out := make([]communi.ToolSchema, 0, len(tc.tools))
	for _, t := range tc.tools {
		out = append(out, communi.ToolSchema{
			Name:        t.Name(),
			Description: t.Description(),
			JSONSchema:  t.JSONSchema(),
		})
	}
	return out
}

// validateToolArguments validates args against schema using JSON Schema Draft-7 semantics
// (github.com/xeipuuv/gojsonschema, AJV-style). schema must describe a JSON object (type: object).
func validateToolArguments(toolName string, schema map[string]any, args json.RawMessage) error {
	if schema == nil {
		schema = map[string]any{"type": "object"}
	}
	// Ensure document is JSON; empty normalizes to {}.
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	var probe any
	if err := json.Unmarshal(args, &probe); err != nil {
		return &ToolValidationError{ToolName: toolName, Details: []string{"arguments must be valid JSON: " + err.Error()}}
	}

	schemaLoader := gojsonschema.NewGoLoader(schema)
	docLoader := gojsonschema.NewBytesLoader(args)
	result, err := gojsonschema.Validate(schemaLoader, docLoader)
	if err != nil {
		return &ToolValidationError{ToolName: toolName, Details: []string{err.Error()}}
	}
	if result.Valid() {
		return nil
	}
	var details []string
	for _, e := range result.Errors() {
		details = append(details, e.String())
	}
	return &ToolValidationError{ToolName: toolName, Details: details}
}

func (tc *ToolController) RegistryTool(ts ...Tool) {
	if tc.tools == nil {
		tc.tools = make(map[string]Tool)
	}
	for _, t := range ts {
		tc.tools[t.Name()] = t
	}
}

func (tc *ToolController) Tools() []Tool {
	res := make([]Tool, 0, len(tc.tools))
	for _, tool := range tc.tools {
		res = append(res, tool)
	}
	return res
}

// NormalizeToolCallArguments returns a JSON payload suitable for tool execution.
// It ensures non-nil/non-empty (defaults to {}), and unwraps one level of
// double-encoded JSON string when the model returns arguments as a quoted string.
func normalizeToolCallArguments(arg json.RawMessage) json.RawMessage {
	if len(arg) == 0 {
		return json.RawMessage(`{}`)
	}
	trimmed := json.RawMessage(strings.TrimSpace(string(arg)))
	if len(trimmed) == 0 {
		return json.RawMessage(`{}`)
	}
	// Some models return arguments as a JSON string (double-encoded); unwrap once.
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err == nil {
			return json.RawMessage(s)
		}
	}
	return trimmed
}

// ValidateToolArguments validates args against schema using JSON Schema Draft-7 semantics
// (github.com/xeipuuv/gojsonschema, AJV-style). schema must describe a JSON object (type: object).
func ValidateToolArguments(toolName string, schema map[string]any, args json.RawMessage) error {
	if schema == nil {
		schema = map[string]any{"type": "object"}
	}
	// Ensure document is JSON; empty normalizes to {}.
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	var probe any
	if err := json.Unmarshal(args, &probe); err != nil {
		return &ToolValidationError{ToolName: toolName, Details: []string{"arguments must be valid JSON: " + err.Error()}}
	}

	schemaLoader := gojsonschema.NewGoLoader(schema)
	docLoader := gojsonschema.NewBytesLoader(args)
	result, err := gojsonschema.Validate(schemaLoader, docLoader)
	if err != nil {
		return &ToolValidationError{ToolName: toolName, Details: []string{err.Error()}}
	}
	if result.Valid() {
		return nil
	}
	var details []string
	for _, e := range result.Errors() {
		details = append(details, e.String())
	}
	return &ToolValidationError{ToolName: toolName, Details: details}
}

// CoerceToolArguments normalizes common model mistakes before JSON Schema validation:
//   - If args are a JSON string and the schema has exactly one required string property,
//     wrap as a single-field object (e.g. "/path" -> {"path":"/path"}).
//   - If there are no required properties but the schema defines "path", a bare string
//     is wrapped as {"path": "..."} (list directory tool).
func CoerceToolArguments(schema map[string]any, args json.RawMessage) json.RawMessage {
	if len(args) == 0 {
		return json.RawMessage(`{}`)
	}
	var raw any
	if err := json.Unmarshal(args, &raw); err != nil {
		return args
	}
	if m, ok := raw.(map[string]any); ok && m != nil {
		return args
	}
	s, ok := raw.(string)
	if !ok {
		return args
	}

	req := requiredKeys(schema)
	props, _ := schema["properties"].(map[string]any)

	if len(req) == 1 && props != nil {
		key := req[0]
		if prop, ok := props[key].(map[string]any); ok {
			if typ, _ := prop["type"].(string); typ == "string" {
				out, err := json.Marshal(map[string]string{key: s})
				if err == nil {
					return out
				}
			}
		}
	}

	if len(req) == 0 && props != nil {
		if _, hasPath := props["path"]; hasPath {
			out, err := json.Marshal(map[string]string{"path": s})
			if err == nil {
				return out
			}
		}
	}

	return args
}

func requiredKeys(schema map[string]any) []string {
	req, ok := schema["required"]
	if !ok || req == nil {
		return nil
	}
	switch x := req.(type) {
	case []string:
		return append([]string(nil), x...)
	case []any:
		var out []string
		for _, v := range x {
			if s, ok := v.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// ErrUnknownProvider is returned when no provider is registered for a given name.
type ErrUnknownProvider string

func (e ErrUnknownProvider) Error() string {
	return fmt.Sprintf("unknown provider: %s", string(e))
}

// ToolValidationError is returned when tool arguments do not satisfy the tool's JSON Schema.
// Prefer returning this from the validation layer; the agent turns it into an isError toolResult
// for the model (see executePendingTools), rather than aborting the turn.
type ToolValidationError struct {
	ToolName string
	Details  []string
}

func (e *ToolValidationError) Error() string {
	if e == nil {
		return ""
	}
	msg := fmt.Sprintf("tool argument validation failed for %s", e.ToolName)
	if len(e.Details) == 0 {
		return msg
	}
	return msg + ": " + strings.Join(e.Details, "; ")
}

// ToolUpdateFunc is used by tools to report streaming progress.
type ToolUpdateFunc func(update ToolUpdate)

// ToolUpdate describes an incremental update from a running tool.
type ToolUpdate struct {
	Text     string         // human readable update text
	Progress float64        // optional progress 0..1
	Metadata map[string]any // arbitrary metadata
}
