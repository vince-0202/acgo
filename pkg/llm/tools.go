package llm

import (
	"encoding/json"
	"strings"
)

// Tool describes a callable function exposed to the model.
// JSONSchema is a generic JSON Schema for the parameters, compatible with gojsonschema.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	JSONSchema  map[string]any `json:"json_schema"`
}

// ToolCall represents a model-requested tool invocation.
//
// Arguments convention: always JSON (object or array) as raw bytes. During streaming
// it may be partial; use NormalizeToolCallArguments before execution to get a
// stable, executable payload (handles empty, nil, and double-encoded string).
type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"` // raw JSON; may be partial during streaming
}

// NormalizeToolCallArguments returns a JSON payload suitable for tool execution.
// It ensures non-nil/non-empty (defaults to {}), and unwraps one level of
// double-encoded JSON string when the model returns arguments as a quoted string.
func NormalizeToolCallArguments(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	trimmed := json.RawMessage(strings.TrimSpace(string(raw)))
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

// ToolCallAccumulator accumulates tool call argument deltas from streaming
// (e.g. toolcall_delta) so that a complete ToolCall with normalized Arguments
// can be built. Providers that emit EventToolCallDelta should use this to
// accumulate and then set Event.ToolCall with the result of Build().
type ToolCallAccumulator struct {
	ID      string
	Name    string
	argsBuf strings.Builder
}

// AppendArgumentsDelta appends a chunk of the arguments JSON (e.g. from SSE delta).
func (a *ToolCallAccumulator) AppendArgumentsDelta(delta string) {
	a.argsBuf.WriteString(delta)
}

// Build returns a ToolCall with Arguments set to the accumulated buffer,
// normalized so it is always valid for execution (empty → {}).
func (a *ToolCallAccumulator) Build() ToolCall {
	raw := json.RawMessage(a.argsBuf.String())
	return ToolCall{
		ID:        a.ID,
		Name:      a.Name,
		Arguments: NormalizeToolCallArguments(raw),
	}
}

// ToolResult represents the result of executing a tool.
type ToolResult struct {
	ToolCallID string         `json:"tool_call_id"`
	Content    []ContentBlock `json:"content"`
	IsError    bool           `json:"is_error,omitempty"`
}
