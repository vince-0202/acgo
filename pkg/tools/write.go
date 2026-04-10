package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/vince-0202/acgo/pkg/communi"
	"github.com/vince-0202/acgo/pkg/harness"
	"os"
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
				"minLength":   1,
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

func (t *writeTool) Execute(ctx context.Context, toolCallID string, args json.RawMessage, update harness.ToolUpdateFunc) communi.ToolCallResult {
	var params struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return communi.ErrorToolCallResult(toolCallID, err)
	}
	path := harness.ResolveToolPath(ctx, params.Path)
	if err := os.WriteFile(path, []byte(params.Content), 0o644); err != nil {
		return communi.ErrorToolCallResult(toolCallID, err)
	}
	return communi.NewToolCallResult(
		toolCallID,
		fmt.Sprintf("wrote %d bytes to %s", len(params.Content), path),
	)
}

// NewWriteTool creates a new write AgentTool.
func NewWriteTool() harness.Tool {
	return &writeTool{}
}
