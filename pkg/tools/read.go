package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"go-pi/pkg/agent"
)

type readTool struct{}

func (t *readTool) Name() string        { return "read" }
func (t *readTool) Label() string       { return "Read File" }
func (t *readTool) Description() string { return "Read file contents from disk." }

func (t *readTool) JSONSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path to the file to read",
			},
		},
		"required": []string{"path"},
	}
}

func (t *readTool) Execute(ctx context.Context, toolCallID string, args json.RawMessage, update agent.ToolUpdateFunc) (agent.ToolResult, error) {
	var params struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return agent.ToolResult{}, fmt.Errorf("invalid arguments: %w", err)
	}
	if params.Path == "" {
		return agent.ToolResult{}, fmt.Errorf("path is required")
	}
	data, err := os.ReadFile(params.Path)
	if err != nil {
		return agent.ToolResult{}, err
	}
	return agent.ToolResult{
		Content: string(data),
	}, nil
}

// NewReadTool creates a new read AgentTool.
func NewReadTool() agent.AgentTool {
	return &readTool{}
}

