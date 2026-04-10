package tools

import (
	"context"
	"encoding/json"
	"github.com/vince-0202/acgo/pkg/communi"
	"github.com/vince-0202/acgo/pkg/harness"
	"os/exec"
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

func (t *bashTool) Execute(ctx context.Context, toolCallID string, args json.RawMessage, update harness.ToolUpdateFunc) communi.ToolCallResult {
	var params struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		var raw string
		if json.Unmarshal(args, &raw) == nil && raw != "" {
			params.Command = raw
		} else {
			return communi.ErrorToolCallResult(toolCallID, err)
		}
	}

	cmd := exec.CommandContext(ctx, "bash", "-lc", params.Command)
	if wd := harness.ToolWorkingDirFromContext(ctx); wd != "" {
		cmd.Dir = wd
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return communi.ErrorToolCallResult(toolCallID, err)
	}
	return communi.NewToolCallResult(toolCallID, string(out))
}

// NewBashTool creates a new bash AgentTool.
func NewBashTool() harness.Tool {
	return &bashTool{}
}
