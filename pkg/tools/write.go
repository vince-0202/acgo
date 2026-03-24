package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/vince-0202/acgo/pkg/agent"
)

type writeTool struct{}

func (t *writeTool) Name() string  { return "write" }
func (t *writeTool) Label() string { return "Write FilePath" }
func (t *writeTool) Description() string {
	return "Write contents to a file, replacing existing content if any. Use for creating or overwriting files."
}

func (t *writeTool) JSONSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Absolute or relative path to the file to write",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "Full new file contents to write",
			},
		},
		"required": []string{"path", "content"},
	}
}

func (t *writeTool) Execute(ctx context.Context, toolCallID string, args json.RawMessage, update agent.ToolUpdateFunc) (agent.ToolResult, error) {
	var params struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return agent.ToolResult{}, fmt.Errorf("invalid arguments: %w", err)
	}
	if params.Path == "" {
		return agent.ToolResult{}, fmt.Errorf("path is required")
	}
	if err := os.WriteFile(params.Path, []byte(params.Content), 0o644); err != nil {
		return agent.ToolResult{Content: err.Error(), IsError: true}, nil
	}
	return agent.ToolResult{
		Content: fmt.Sprintf("wrote %d bytes to %s", len(params.Content), params.Path),
	}, nil
}

// NewWriteTool creates a new write AgentTool.
func NewWriteTool() agent.AgentTool {
	return &writeTool{}
}
