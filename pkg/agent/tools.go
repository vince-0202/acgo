package agent

import (
	"context"
	"encoding/json"
)

// ToolUpdate describes an incremental update from a running tool.
type ToolUpdate struct {
	Text     string         // human readable update text
	Progress float64        // optional progress 0..1
	Metadata map[string]any // arbitrary metadata
}

// ToolUpdateFunc is used by tools to report streaming progress.
type ToolUpdateFunc func(update ToolUpdate)

// ToolResult is the final result of a tool execution.
type ToolResult struct {
	Content  string         // text content summarizing the result
	IsError  bool           // whether the tool failed
	Metadata map[string]any // optional metadata
}

// AgentTool is the interface implemented by all tools usable by the Agent.
type AgentTool interface {
	Name() string
	Label() string
	Description() string
	JSONSchema() map[string]any
	Execute(ctx context.Context, toolCallID string, args json.RawMessage, update ToolUpdateFunc) (ToolResult, error)
}

