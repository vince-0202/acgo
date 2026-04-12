package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/vince-0202/acgo/pkg/agent"
	"github.com/vince-0202/acgo/pkg/runtime"
	"strings"

	"github.com/vince-0202/acgo/pkg/communi"
)

type cronListTool struct{}

func (t *cronListTool) Name() string  { return "cron_list" }
func (t *cronListTool) Label() string { return "Cron List" }
func (t *cronListTool) Description() string {
	return "List all in-session cron tasks and their status."
}

func (t *cronListTool) JSONSchema() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
		"required":   []string{},
	}
}

func (t *cronListTool) Execute(ctx context.Context, toolCallID string, args json.RawMessage, update agent.ToolUpdateFunc) communi.ToolCallResult {
	tasks := runtime.DefaultCronManager.List()
	if len(tasks) == 0 {
		return communi.NewToolCallResult(toolCallID, "no cron tasks")
	}
	var b strings.Builder
	for _, task := range tasks {
		fmt.Fprintf(&b, "id=%s agent_id=%s status=%s runs=%d", task.ID, task.AgentID, task.Status, task.RunCount)
		if !task.NextRunAt.IsZero() {
			fmt.Fprintf(&b, " next_run_at=%s", task.NextRunAt.Format("2006-01-02 15:04:05"))
		}
		if task.IntervalSeconds > 0 {
			fmt.Fprintf(&b, " interval_seconds=%d", task.IntervalSeconds)
		}
		if task.MaxRuns > 0 {
			fmt.Fprintf(&b, " max_runs=%d", task.MaxRuns)
		}
		b.WriteString("\n")
		fmt.Fprintf(&b, "prompt=%s\n\n", task.Prompt)
	}
	return communi.NewToolCallResult(toolCallID, strings.TrimSpace(b.String()))
}

func NewCronListTool() agent.Tool {
	return &cronListTool{}
}
