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

// Tool is the interface implemented by all tools usable by the Agent.
type Tool interface {
	Name() string
	Label() string
	Description() string
	JSONSchema() map[string]any
	Execute(ctx context.Context, toolCallID string, args json.RawMessage, update ToolUpdateFunc) communi.ToolCallResult
}

func NewToolController(agentId string, emf func(e communi.AgentEvent), amf func(msg ...communi.Message), tools ...Tool) *ToolController {
	toolMap := make(map[string]Tool)
	for _, tool := range tools {
		toolMap[tool.Name()] = tool
	}
	return &ToolController{
		agentId:           agentId,
		tools:             toolMap,
		pendingToolCalls:  make([]communi.ToolCallRequest, 0),
		emitFunc:          emf,
		appendMessageFunc: amf,
	}
}

type ToolController struct {
	agentId           string
	tools             map[string]Tool
	pendingToolCalls  []communi.ToolCallRequest
	emitFunc          func(e communi.AgentEvent)
	appendMessageFunc func(msg ...communi.Message)
}

func (c *ToolController) CleanPendingTool() {
	c.pendingToolCalls = make([]communi.ToolCallRequest, 0)
}

func (c *ToolController) Execute(ctx context.Context) {
	if c.pendingToolCalls == nil {
		return
	}
	calls := c.pendingToolCalls
	c.pendingToolCalls = nil

	for _, call := range calls {
		args := normalizeToolCallArguments(call.Arguments)
		tool, ok := c.FindTool(call.Name)
		if !ok {
			toolMsg := communi.NewToolCallErrorMessage(
				call.ID, ErrUnknownProvider(call.Name),
			)
			c.appendMessageFunc(toolMsg)
			c.emitFunc(communi.AgentEvent{
				Type:       communi.EventToolExecutionEnd,
				AgentID:    c.agentId,
				ToolName:   call.Name,
				ToolCallID: call.ID,
				ToolArgs:   args,
				Message:    &toolMsg,
				Error:      ErrUnknownProvider(call.Name),
				ErrorKind:  errors.ErrKindTool,
			})
		}
		args = CoerceToolArguments(tool.JSONSchema(), args)
		c.emitFunc(communi.AgentEvent{
			Type:       communi.EventToolExecutionStart,
			AgentID:    c.agentId,
			ToolName:   tool.Name(),
			ToolCallID: call.ID,
			ToolArgs:   args,
		})

		if err := ValidateToolArguments(tool.Name(), tool.JSONSchema(), args); err != nil {
			// Validation failures are delivered as tool results with IsError so the model can retry.
			toolMsg := communi.NewToolCallErrorMessage(call.ID, err)
			c.appendMessageFunc(toolMsg)
			c.emitFunc(communi.AgentEvent{
				Type:       communi.EventToolExecutionEnd,
				AgentID:    c.agentId,
				ToolName:   tool.Name(),
				ToolCallID: call.ID,
				ToolArgs:   args,
				Message:    &toolMsg,
				Error:      err,
				ErrorKind:  errors.ErrKindTool,
			})
			continue
		}

		result := tool.Execute(ctx, call.ID, args, nil)
		if result.IsError() {
			toolMsg := communi.NewToolCallErrorMessage(call.ID, result.Error)
			c.appendMessageFunc(toolMsg)
			c.emitFunc(communi.AgentEvent{
				Type:       communi.EventToolExecutionEnd,
				AgentID:    c.agentId,
				ToolName:   tool.Name(),
				ToolCallID: call.ID,
				ToolArgs:   args,
				Message:    &toolMsg,
				Error:      result.Error,
				ErrorKind:  errors.ErrKindTool,
			})
			continue
		}

		toolMsg := communi.Message{
			ID:      "tool-" + call.ID,
			Role:    keys.AgentRoleTool,
			Content: result.Content,
			ToolCall: &communi.ToolCallRequest{
				ID: call.ID,
			},
			IsError:  result.IsError(),
			Metadata: result.Metadata,
		}
		c.appendMessageFunc(toolMsg)
		c.emitFunc(communi.AgentEvent{
			Type:       communi.EventToolExecutionEnd,
			AgentID:    c.agentId,
			ToolName:   tool.Name(),
			ToolCallID: call.ID,
			ToolArgs:   args,
			Message:    &toolMsg,
			Error:      result.Error,
			ErrorKind:  errors.ErrKindTool,
		})

	}
}

func (c ToolController) GetPendingToolCalls() []communi.ToolCallRequest {
	return c.pendingToolCalls
}

func (c *ToolController) FindTool(name string) (tool Tool, ok bool) {
	tool, ok = c.tools[name]
	return
}

// UpsertPendingToolCall tracks the latest version of a ToolCall by ID.
// Stored Arguments are always normalized so they are non-nil and usable for execution.
func (c *ToolController) UpsertPendingToolCall(call communi.ToolCallRequest) {
	normalized := communi.ToolCallRequest{
		ID:        call.ID,
		Name:      call.Name,
		Arguments: llm.NormalizeToolCallArguments(call.Arguments),
	}
	for i := range c.pendingToolCalls {
		if c.pendingToolCalls[i].ID == call.ID {
			c.pendingToolCalls[i] = normalized
			return
		}
	}
	c.pendingToolCalls = append(c.pendingToolCalls, normalized)
}

func (c *ToolController) GetToolSchemas() []communi.ToolSchema {
	if len(c.tools) == 0 {
		return nil
	}
	out := make([]communi.ToolSchema, 0, len(c.tools))
	for _, t := range c.tools {
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

func (c *ToolController) RegistryTool(ts ...Tool) {
	if c.tools == nil {
		c.tools = make(map[string]Tool)
	}
	for _, t := range ts {
		c.tools[t.Name()] = t
	}
}

func (c *ToolController) Tools() []Tool {
	res := make([]Tool, 0, len(c.tools))
	for _, tool := range c.tools {
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
