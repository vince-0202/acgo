package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"

	"acgo/pkg/agent"
)

type bashTool struct{}

func (t *bashTool) Name() string  { return "bash" }
func (t *bashTool) Label() string { return "Run Shell Command" }
func (t *bashTool) Description() string {
	return "Execute a shell command and return its stdout/stderr."
}

func (t *bashTool) JSONSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "Shell command to execute",
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
		// Some models pass a raw string instead of {"command":"..."}; treat it as the command.
		var raw string
		if json.Unmarshal(args, &raw) == nil && raw != "" {
			params.Command = raw
		} else {
			return agent.ToolResult{}, fmt.Errorf("invalid arguments: %w", err)
		}
	}
	if params.Command == "" {
		return agent.ToolResult{}, fmt.Errorf("command is required")
	}

	cmd := exec.CommandContext(ctx, "bash", "-lc", params.Command)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Return error via Go error channel; content only on success.
		return agent.ToolResult{}, fmt.Errorf("command failed: %w; output: %s", err, string(out))
	}
	return agent.ToolResult{
		Content: string(out),
	}, nil
}

// NewBashTool creates a new bash AgentTool.
func NewBashTool() agent.AgentTool {
	return &bashTool{}
}
