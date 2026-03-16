package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"acgo/pkg/agent"
)

type editTool struct{}

func (t *editTool) Name() string  { return "edit" }
func (t *editTool) Label() string { return "Edit File" }
func (t *editTool) Description() string {
	return "Edit a file by replacing its contents. Provide the full new content; for small changes you can read the file first, then write the modified content here."
}

func (t *editTool) JSONSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path to the file to edit",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "Full new file contents (replaces existing file)",
			},
			"instructions": map[string]any{
				"type":        "string",
				"description": "Optional natural-language edit instructions (for reference; apply changes and put result in content)",
			},
		},
		"required": []string{"path", "content"},
	}
}

func (t *editTool) Execute(ctx context.Context, toolCallID string, args json.RawMessage, update agent.ToolUpdateFunc) (agent.ToolResult, error) {
	var params struct {
		Path         string `json:"path"`
		Content      string `json:"content"`
		Instructions string `json:"instructions"`
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
		Content: fmt.Sprintf("edited %s (%d bytes)", params.Path, len(params.Content)),
	}, nil
}

// NewEditTool creates a new edit AgentTool.
func NewEditTool() agent.AgentTool {
	return &editTool{}
}
