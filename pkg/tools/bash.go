package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"

	"github.com/vince-0202/acgo/pkg/agent"
)

type bashTool struct{}

func (t *bashTool) Name() string  { return "bash" }
func (t *bashTool) Label() string { return "Run Shell Command" }
func (t *bashTool) Description() string {
	return "Execute a shell command and return its combined stdout and stderr. Use for running scripts, listing dirs, or any shell operation."
}

func (t *bashTool) JSONSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"minLength":   1,
				"description": "Shell command to execute (e.g. 'ls -la', 'pwd')",
			},
		},
		"required": []string{"command"},
	}
}

func (t *bashTool) Execute(ctx context.Context, toolCallID string, args json.RawMessage, update agent.ToolUpdateFunc) (agent.ToolResult, error) {
	var params struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		var raw string
		if json.Unmarshal(args, &raw) == nil && raw != "" {
			params.Command = raw
		} else {
			return agent.ToolResult{}, fmt.Errorf("invalid arguments: %w", err)
		}
	}

	cmd := exec.CommandContext(ctx, "bash", "-lc", params.Command)
	out, err := cmd.CombinedOutput()
	if err != nil {
		content := string(out)
		if content != "" {
			content += "\n"
		}
		content += err.Error()
		return agent.ToolResult{Content: content, IsError: true}, nil
	}
	return agent.ToolResult{
		Content: string(out),
	}, nil
}

// NewBashTool creates a new bash AgentTool.
func NewBashTool() agent.AgentTool {
	return &bashTool{}
}
