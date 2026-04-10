package agent_new

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

// ToolUpdateFunc is used by tools to report streaming progress.
type ToolUpdateFunc func(update ToolUpdate)

// ExecuteToolOptions controls behavior for a single tool execution.
type ExecuteToolOptions struct {
	// AppendTranscript controls whether the tool result message is appended
	// into the current conversation context.
	AppendTranscript bool
	// EmitEvents controls whether tool execution start/end events are emitted.
	EmitEvents bool
	// Update receives streaming progress updates from the tool.
	Update ToolUpdateFunc
}

// ToolUpdate describes an incremental update from a running tool.
type ToolUpdate struct {
	Text     string         // human readable update text
	Progress float64        // optional progress 0..1
	Metadata map[string]any // arbitrary metadata
}

type toolsManager struct {
	tools            map[string]Tool
	pendingToolCalls []communi.ToolCallRequest
}

func (tm *toolsManager) ClearPendingTool() {
	tm.pendingToolCalls = make([]communi.ToolCallRequest, 0)
}

func (tm *toolsManager) GetToolSchemas() []communi.ToolSchema {
	if len(tm.tools) == 0 {
		return nil
	}
	out := make([]communi.ToolSchema, 0, len(tm.tools))
	for _, t := range tm.tools {
		out = append(out, communi.ToolSchema{
			Name:        t.Name(),
			Description: t.Description(),
			JSONSchema:  t.JSONSchema(),
		})
	}
	return out
}

func (tm *toolsManager) UpsertPendingToolCall(call communi.ToolCallRequest) {
	normalized := communi.ToolCallRequest{
		ID:        call.ID,
		Name:      call.Name,
		Arguments: llm.NormalizeToolCallArguments(call.Arguments),
	}
	for i := range tm.pendingToolCalls {
		if tm.pendingToolCalls[i].ID == call.ID {
			tm.pendingToolCalls[i] = normalized
			return
		}
	}
	tm.pendingToolCalls = append(tm.pendingToolCalls, normalized)
}

func (tm *toolsManager) GetPendingToolCalls() []communi.ToolCallRequest {
	return tm.pendingToolCalls
}

func (tm *toolsManager) FindTool(name string) (tool Tool, ok bool) {
	tool, ok = tm.tools[name]
	return
}

func (tm *toolsManager) Execute(ctx context.Context, turn *turn) {
	if len(tm.pendingToolCalls) == 0 {
		return
	}
	calls := tm.pendingToolCalls
	tm.pendingToolCalls = make([]communi.ToolCallRequest, 0)
	ctx = context.WithValue(ctx, toolExecutorContextKey{}, tm)
	options := ExecuteToolOptions{
		AppendTranscript: true,
		EmitEvents:       true,
	}

	for _, call := range calls {
		tool, ok := tm.FindTool(call.Name)
		if !ok {
			err := ErrUnknownProvider(call.Name)
			res := communi.ErrorToolCallResult(call.ID, err)
			finalizeToolExecution(turn.agent, tool, call, call.Arguments, res, options)
			continue
		}

		executor := ToolExecutor{
			agent:  turn.agent,
			turn:   turn,
			tool:   tool,
			caller: call,
			opts:   options,
		}
		_, _ = executor.ExecuteByName(ctx, options)
	}
}

type ToolExecutor struct {
	turn   *turn
	agent  *Agent
	caller communi.ToolCallRequest
	tool   Tool
	opts   ExecuteToolOptions
}

type toolExecutorContextKey struct{}

// ExecuteByName executes a single tool call by tool name and arguments.
// It reuses argument normalization/coercion/validation and permission checks.
func (te *ToolExecutor) ExecuteByName(ctx context.Context, opts ExecuteToolOptions) (communi.ToolCallResult, error) {

	toolCall := communi.ToolCallRequest{
		ID:        te.caller.ID,
		Name:      te.caller.Name,
		Arguments: normalizeToolCallArguments(te.caller.Arguments),
	}

	toolArgs := CoerceToolArguments(te.tool.JSONSchema(), toolCall.Arguments)
	toolCall.Arguments = toolArgs
	if opts.EmitEvents && te.agent != nil {
		te.agent.emit(NewEvent(
			WithEventType(EventToolExecutionStart),
			WithEventAgent(te.agent),
			WithEventTooCallId(te.caller.ID),
			WithEventTool(te.tool),
		))

	}

	if err := ValidateToolArguments(te.tool.Name(), te.tool.JSONSchema(), toolArgs); err != nil {
		res := communi.ErrorToolCallResult(te.caller.ID, err)
		finalizeToolExecution(te.agent, te.tool, toolCall, toolArgs, res, opts)
		return res, err
	}

	res := te.tool.Execute(ctx, te.caller.ID, toolArgs, opts.Update)
	finalizeToolExecution(te.agent, te.tool, toolCall, toolArgs, res, opts)
	return res, res.Error
}

func finalizeToolExecution(agent *Agent, tool Tool, call communi.ToolCallRequest, args json.RawMessage, result communi.ToolCallResult, opts ExecuteToolOptions) {
	msg, errVal := toolResultToMessage(call, args, result)
	if opts.AppendTranscript {
		agent.context.AppendMessage(msg)
	}
	if opts.EmitEvents && agent != nil {
		agent.emit(NewEvent(
			WithEventType(EventToolExecutionEnd),
			WithEventAgent(agent),
			WithEventTool(tool),
			WithEventMessage(&msg),
			WithEventError(errors.WrapError(errVal)),
		))
	}
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

// ErrUnknownProvider is returned when no provider is registered for a given name.
type ErrUnknownProvider string

func (e ErrUnknownProvider) Error() string {
	return fmt.Sprintf("unknown provider: %s", string(e))
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

func toolResultToMessage(call communi.ToolCallRequest, args json.RawMessage, result communi.ToolCallResult) (communi.Message, error) {
	if result.IsError() {
		toolMsg := communi.NewToolCallErrorMessage(call.ID, result.Error)
		toolMsg.ToolCall.Name = call.Name
		toolMsg.ToolCall.Arguments = args
		return toolMsg, result.Error
	}
	toolMsg := communi.Message{
		ID:      "tool-" + call.ID,
		Role:    keys.AgentRoleTool,
		Content: result.Content,
		ToolCall: &communi.ToolCallRequest{
			ID:        call.ID,
			Name:      call.Name,
			Arguments: args,
		},
		IsError:  result.IsError(),
		Metadata: result.Metadata,
	}
	return toolMsg, nil
}
