package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/vince-0202/acgo/pkg/agent"
	"strings"

	"github.com/vince-0202/acgo/pkg/runtime"

	"github.com/vince-0202/acgo/pkg/communi"
)

type cronCreateTool struct{}

func (t *cronCreateTool) Name() string  { return "cron_create" }
func (t *cronCreateTool) Label() string { return "Cron Create" }
func (t *cronCreateTool) Description() string {
	return "Create an in-session scheduled task. Supports one-shot delay or repeating interval."
}

func (t *cronCreateTool) JSONSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"agent_id": map[string]any{
				"type":        "string",
				"description": "Target agent ID to execute this scheduled prompt. Defaults to first registered agent.",
			},
			"prompt": map[string]any{
				"type":        "string",
				"minLength":   1,
				"description": "Task prompt/description for the scheduled job.",
			},
			"delay_seconds": map[string]any{
				"type":        "number",
				"description": "Delay before first run in seconds (default: 0).",
			},
			"interval_seconds": map[string]any{
				"type":        "number",
				"description": "Repeat interval in seconds. If omitted/0, the task runs once.",
			},
			"max_runs": map[string]any{
				"type":        "number",
				"description": "Maximum runs for repeating tasks. <=0 means unlimited repeats.",
			},
		},
		"required": []string{"prompt"},
	}
}

func (t *cronCreateTool) Execute(ctx context.Context, toolCallID string, args json.RawMessage, update agent.ToolUpdateFunc) communi.ToolCallResult {
	var params struct {
		AgentID         string  `json:"agent_id"`
		Prompt          string  `json:"prompt"`
		DelaySeconds    float64 `json:"delay_seconds"`
		IntervalSeconds float64 `json:"interval_seconds"`
		MaxRuns         float64 `json:"max_runs"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return communi.ErrorToolCallResult(toolCallID, err)
	}
	prompt := strings.TrimSpace(params.Prompt)
	if prompt == "" {
		return communi.ErrorToolCallResult(toolCallID, fmt.Errorf("prompt is required"))
	}

	agentID := strings.TrimSpace(params.AgentID)
	if agentID == "" {
		agents := runtime.ListAgents()
		if len(agents) == 0 {
			return communi.ErrorToolCallResult(toolCallID, fmt.Errorf("no registered agent available for cron task"))
		}
		agentID = agents[0].ID()
	}
	if _, ok := runtime.GetAgent(agentID); !ok {
		return communi.ErrorToolCallResult(toolCallID, fmt.Errorf("agent not found: %s", agentID))
	}

	task := runtime.DefaultCronManager.Create(agentID, prompt, int(params.DelaySeconds), int(params.IntervalSeconds), int(params.MaxRuns))
	return communi.NewToolCallResult(toolCallID, fmt.Sprintf(
		"created cron task id=%s agent_id=%s status=%s next_run_at=%s interval_seconds=%d max_runs=%d",
		task.ID,
		task.AgentID,
		task.Status,
		task.NextRunAt.Format("2006-01-02 15:04:05"),
		task.IntervalSeconds,
		task.MaxRuns,
	))
}

func NewCronCreateTool() agent.Tool {
	return &cronCreateTool{}
}
