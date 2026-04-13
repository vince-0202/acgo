package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/vince-0202/acgo/pkg/communi"
	"github.com/vince-0202/acgo/pkg/errors"
	"github.com/vince-0202/acgo/pkg/keys"
	"github.com/vince-0202/acgo/pkg/llm"
	"github.com/xeipuuv/gojsonschema"
	"path/filepath"
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

type ToolExecutionRequest struct {
	Agent    *Agent
	TurnID   string
	Tool     Tool
	ToolCall communi.ToolCallRequest
	Args     json.RawMessage
}

type ToolExecutionHandler func(ctx context.Context, req ToolExecutionRequest) (communi.ToolCallResult, error)

type ToolExecutionMiddleware func(ctx context.Context, req ToolExecutionRequest, next ToolExecutionHandler) (communi.ToolCallResult, error)

type ToolDispatcher interface {
	ExecuteByName(ctx context.Context, callID, toolName string, args json.RawMessage, opts ExecuteToolOptions) (communi.ToolCallResult, error)
}

type toolsManager struct {
	tools            map[string]Tool
	pendingToolCalls []communi.ToolCallRequest
	middlewares      []ToolExecutionMiddleware
}

func (tm *toolsManager) RegisterTool(tool Tool) {
	if tm == nil || tool == nil {
		return
	}
	if tm.tools == nil {
		tm.tools = make(map[string]Tool)
	}
	tm.tools[tool.Name()] = tool
}

func (tm *toolsManager) UnregisterTool(name string) {
	if tm == nil || tm.tools == nil {
		return
	}
	delete(tm.tools, name)
}

func (tm *toolsManager) RegisteredTools() []Tool {
	if tm == nil || len(tm.tools) == 0 {
		return nil
	}
	out := make([]Tool, 0, len(tm.tools))
	for _, tool := range tm.tools {
		out = append(out, tool)
	}
	return out
}

func (tm *toolsManager) RegisterMiddleware(mw ToolExecutionMiddleware) func() {
	if tm == nil || mw == nil {
		return func() {}
	}
	tm.middlewares = append(tm.middlewares, mw)
	idx := len(tm.middlewares) - 1
	return func() {
		if idx < 0 || idx >= len(tm.middlewares) || tm.middlewares[idx] == nil {
			return
		}
		tm.middlewares[idx] = nil
	}
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
	ctx = ContextWithToolDispatcher(ctx, tm)
	options := ExecuteToolOptions{
		AppendTranscript: true,
		EmitEvents:       true,
	}

	for _, call := range calls {
		tool, ok := tm.FindTool(call.Name)
		if !ok {
			err := ErrUnknownProvider(call.Name)
			res := communi.ErrorToolCallResult(call.ID, err)
			finalizeToolExecution(turn.agent, turn.id, tool, call, call.Arguments, res, options)
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

func (tm *toolsManager) ExecuteByName(ctx context.Context, callID, toolName string, args json.RawMessage, opts ExecuteToolOptions) (communi.ToolCallResult, error) {
	if tm == nil {
		res := communi.ErrorToolCallResult(callID, fmt.Errorf("tool manager is nil"))
		return res, res.Error
	}
	tool, ok := tm.FindTool(toolName)
	if !ok {
		res := communi.ErrorToolCallResult(callID, ErrUnknownProvider(toolName))
		return res, res.Error
	}
	executor := ToolExecutor{
		tool: tool,
		caller: communi.ToolCallRequest{
			ID:        callID,
			Name:      toolName,
			Arguments: args,
		},
		opts: opts,
	}
	return executor.ExecuteByName(ctx, opts)
}

type ToolExecutor struct {
	turn   *turn
	agent  *Agent
	caller communi.ToolCallRequest
	tool   Tool
	opts   ExecuteToolOptions
}

type toolExecutorContextKey struct{}

// ContextWithToolDispatcher attaches a dispatcher for nested tool execution.
func ContextWithToolDispatcher(ctx context.Context, dispatcher ToolDispatcher) context.Context {
	if ctx == nil || dispatcher == nil {
		return ctx
	}
	return context.WithValue(ctx, toolExecutorContextKey{}, dispatcher)
}

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
			WithEventTurnId(func() string {
				if te.turn != nil {
					return te.turn.id
				}
				return ""
			}()),
			WithEventTooCallId(te.caller.ID),
			WithEventTool(te.tool),
		))

	}

	if err := ValidateToolArguments(te.tool.Name(), te.tool.JSONSchema(), toolArgs); err != nil {
		res := communi.ErrorToolCallResult(te.caller.ID, err)
		finalizeToolExecution(te.agent, reqTurnID(te.turn), te.tool, toolCall, toolArgs, res, opts)
		return res, err
	}

	req := ToolExecutionRequest{
		Agent:    te.agent,
		TurnID:   "",
		Tool:     te.tool,
		ToolCall: toolCall,
		Args:     toolArgs,
	}
	if te.turn != nil {
		req.TurnID = te.turn.id
	}
	if te.agent != nil {
		te.agent.emit(NewEvent(
			WithEventType(EventBeforeToolExecution),
			WithEventAgent(te.agent),
			WithEventTurnId(req.TurnID),
			WithEventTooCallId(te.caller.ID),
			WithEventTool(te.tool),
		))
	}
	executeCore := func(ctx context.Context, req ToolExecutionRequest) (communi.ToolCallResult, error) {
		result := te.tool.Execute(ctx, te.caller.ID, toolArgs, opts.Update)
		return result, result.Error
	}

	var (
		res communi.ToolCallResult
		err error
	)
	if te.agent != nil && te.agent.toolManager != nil {
		res, err = te.agent.toolManager.executeWithMiddleware(ctx, req, executeCore)
	} else {
		res, err = executeCore(ctx, req)
	}
	finalizeToolExecution(te.agent, req.TurnID, te.tool, toolCall, toolArgs, res, opts)
	return res, err
}

func (tm *toolsManager) executeWithMiddleware(ctx context.Context, req ToolExecutionRequest, core ToolExecutionHandler) (communi.ToolCallResult, error) {
	if tm == nil {
		return core(ctx, req)
	}
	handler := core
	for i := len(tm.middlewares) - 1; i >= 0; i-- {
		mw := tm.middlewares[i]
		if mw == nil {
			continue
		}
		next := handler
		handler = func(ctx context.Context, req ToolExecutionRequest) (communi.ToolCallResult, error) {
			return mw(ctx, req, next)
		}
	}
	return handler(ctx, req)
}

func finalizeToolExecution(agent *Agent, turnID string, tool Tool, call communi.ToolCallRequest, args json.RawMessage, result communi.ToolCallResult, opts ExecuteToolOptions) {
	msg, errVal := toolResultToMessage(call, args, result)
	if opts.AppendTranscript {
		agent.context.AppendMessage(msg)
	}
	if opts.EmitEvents && agent != nil {
		agent.emit(NewEvent(
			WithEventType(EventToolExecutionEnd),
			WithEventAgent(agent),
			WithEventTurnId(turnID),
			WithEventTool(tool),
			WithEventTooCallId(call.ID),
			WithEventMessage(&msg),
			WithEventError(errors.WrapError(errVal)),
		))
		agent.emit(NewEvent(
			WithEventType(EventAfterToolExecution),
			WithEventAgent(agent),
			WithEventTurnId(turnID),
			WithEventTool(tool),
			WithEventTooCallId(call.ID),
			WithEventMessage(&msg),
			WithEventError(errors.WrapError(errVal)),
		))
	}
}

func reqTurnID(t *turn) string {
	if t == nil {
		return ""
	}
	return t.id
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
		toolMsg.Metadata = result.Metadata
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

type toolWorkingDirKey struct{}

// ContextWithToolWorkingDir attaches the directory tools should use as cwd / relative-path base.
func ContextWithToolWorkingDir(ctx context.Context, dir string) context.Context {
	if ctx == nil || strings.TrimSpace(dir) == "" {
		return ctx
	}
	return context.WithValue(ctx, toolWorkingDirKey{}, filepath.Clean(dir))
}

// ToolWorkingDirFromContext returns the tool working directory, or "" if unset.
func ToolWorkingDirFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v := ctx.Value(toolWorkingDirKey{})
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// ResolveToolPath resolves a path for file tools: absolute paths stay as-is; relative paths join the tool working directory when set.
func ResolveToolPath(ctx context.Context, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	wd := ToolWorkingDirFromContext(ctx)
	if wd == "" {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(wd, path))
}

// ToolExecutorFromContext returns a dispatcher injected by the active tool execution pipeline.
func ToolExecutorFromContext(ctx context.Context) (ToolDispatcher, bool) {
	v := ctx.Value(toolExecutorContextKey{})
	if v == nil {
		return nil, false
	}
	te, ok := v.(ToolDispatcher)
	return te, ok
}
