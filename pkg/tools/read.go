package tools

import (
	"context"
	"encoding/json"
	"github.com/vince-0202/acgo/pkg/communi"
	"github.com/vince-0202/acgo/pkg/harness"
	"os"
)

type readTool struct{}

func (t *readTool) Name() string  { return "read" }
func (t *readTool) Label() string { return "Read FilePath" }
func (t *readTool) Description() string {
	return "Read file contents from disk. Use for reading source files, configs, or any text file."
}

func (t *readTool) JSONSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"minLength":   1,
				"description": "Absolute or relative path to the file to read",
			},
		},
		"required": []string{"path"},
	}
}

func (t *readTool) Execute(ctx context.Context, toolCallID string, args json.RawMessage, update harness.ToolUpdateFunc) communi.ToolCallResult {
	var params struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		// Defense-in-depth if Execute is called without agent-side validation.
		var raw string
		if json.Unmarshal(args, &raw) == nil && raw != "" {
			params.Path = raw
		} else {
			return communi.ErrorToolCallResult(toolCallID, err)
		}
	}
	data, err := os.ReadFile(params.Path)
	if err != nil {
		return communi.ErrorToolCallResult(toolCallID, err)
	}
	return communi.NewToolCallResult(toolCallID, string(data))
}

// NewReadTool creates a new read AgentTool.
func NewReadTool() harness.Tool {
	return &readTool{}
}
