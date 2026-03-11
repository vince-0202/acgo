package llm

import "encoding/json"

// Tool describes a callable function exposed to the model.
// JSONSchema is a generic JSON Schema for the parameters, compatible with gojsonschema.
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	JSONSchema  map[string]any         `json:"json_schema"`
}

// ToolCall represents a model-requested tool invocation.
type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"` // raw JSON arguments; may be partial during streaming
}

// ToolResult represents the result of executing a tool.
type ToolResult struct {
	ToolCallID string         `json:"tool_call_id"`
	Content    []ContentBlock `json:"content"`
	IsError    bool           `json:"is_error,omitempty"`
}

