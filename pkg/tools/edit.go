package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/vince-0202/acgo/pkg/communi"
	"github.com/vince-0202/acgo/pkg/harness"
	"os"
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
				"minLength":   1,
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

func (t *editTool) Execute(ctx context.Context, toolCallID string, args json.RawMessage, update harness.ToolUpdateFunc) communi.ToolCallResult {
	var params struct {
		Path         string `json:"path"`
		Content      string `json:"content"`
		Instructions string `json:"instructions"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return communi.ErrorToolCallResult(toolCallID, err)
	}
	path := harness.ResolveToolPath(ctx, params.Path)
	if err := os.WriteFile(path, []byte(params.Content), 0o644); err != nil {
		return communi.ErrorToolCallResult(toolCallID, err)
	}
	return communi.NewToolCallResult(toolCallID, fmt.Sprintf("edited %s (%d bytes)", path, len(params.Content)))
}

// NewEditTool creates a new edit AgentTool.
func NewEditTool() harness.Tool {
	return &editTool{}
}
