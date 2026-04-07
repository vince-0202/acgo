package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/vince-0202/acgo/pkg/communi"
	"github.com/vince-0202/acgo/pkg/harness"
)

type subAgentTool struct {
	rt harness.SubAgentRuntime
}

func newSubAgentTool(rt harness.SubAgentRuntime) harness.Tool {
	return &subAgentTool{rt: rt}
}

func (t *subAgentTool) Name() string  { return subAgentToolName }
func (t *subAgentTool) Label() string { return "Sub-agent" }
func (t *subAgentTool) Description() string {
	return "Manage delegated sub-agents: create a named worker, run a task on it (full turn with tools), list workers, or remove a worker. " +
		"When the model requests multiple tool calls in one turn, those runs execute in parallel. " +
		"Sub-agents share the same model and tools as this agent but have separate conversation state and workspace under subagents/<sub_id>."
}

func (t *subAgentTool) JSONSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"create", "task", "batch_task", "list", "remove"},
				"description": "create: register a sub-agent; task: run one prompt on one sub-agent; batch_task: run multiple sub-agent tasks concurrently; list: list sub-agents; remove: forget a sub-agent handle",
			},
			"sub_id": map[string]any{
				"type":        "string",
				"description": "Logical name for the sub-agent (required for create, task, remove)",
			},
			"prompt": map[string]any{
				"type":        "string",
				"description": "Task instructions for the sub-agent when action is task",
			},
			"tasks": map[string]any{
				"type":        "array",
				"description": "Batch tasks for action=batch_task",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"sub_id": map[string]any{
							"type":        "string",
							"description": "Target sub-agent id",
						},
						"prompt": map[string]any{
							"type":        "string",
							"description": "Task prompt for the target sub-agent",
						},
					},
					"required": []string{"sub_id", "prompt"},
				},
			},
		},
		"required": []string{"action"},
	}
}

func (t *subAgentTool) Execute(ctx context.Context, toolCallID string, args json.RawMessage, update harness.ToolUpdateFunc) communi.ToolCallResult {
	if t == nil || t.rt == nil {
		return communi.ErrorToolCallResult(toolCallID, fmt.Errorf("sub_agent runtime not configured"))
	}
	var params struct {
		Action string `json:"action"`
		SubID  string `json:"sub_id"`
		Prompt string `json:"prompt"`
		Tasks  []struct {
			SubID  string `json:"sub_id"`
			Prompt string `json:"prompt"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return communi.ErrorToolCallResult(toolCallID, err)
	}
	action := strings.TrimSpace(strings.ToLower(params.Action))
	switch action {
	case "create":
		id, err := t.rt.Create(params.SubID)
		if err != nil {
			return communi.ErrorToolCallResult(toolCallID, err)
		}
		return communi.NewToolCallResult(toolCallID, fmt.Sprintf("created sub-agent %q as agent id %s", strings.TrimSpace(params.SubID), id))
	case "task":
		out, err := t.rt.RunTask(ctx, params.SubID, params.Prompt)
		if err != nil {
			return communi.ErrorToolCallResult(toolCallID, err)
		}
		return communi.NewToolCallResult(toolCallID, out)
	case "batch_task":
		if len(params.Tasks) == 0 {
			return communi.ErrorToolCallResult(toolCallID, fmt.Errorf("tasks is empty"))
		}
		type row struct {
			subID  string
			output string
			err    error
		}
		rows := make([]row, len(params.Tasks))
		var wg sync.WaitGroup
		for i := range params.Tasks {
			wg.Add(1)
			task := params.Tasks[i]
			go func(i int) {
				defer wg.Done()
				out, err := t.rt.RunTask(ctx, task.SubID, task.Prompt)
				rows[i] = row{subID: strings.TrimSpace(task.SubID), output: out, err: err}
			}(i)
		}
		wg.Wait()
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].subID < rows[j].subID })
		var b strings.Builder
		hasErr := false
		for i, r := range rows {
			if i > 0 {
				b.WriteString("\n\n")
			}
			if r.err != nil {
				hasErr = true
				fmt.Fprintf(&b, "[%s] error: %v", r.subID, r.err)
				continue
			}
			fmt.Fprintf(&b, "[%s]\n%s", r.subID, strings.TrimSpace(r.output))
		}
		if hasErr {
			return communi.ErrorToolCallResult(toolCallID, fmt.Errorf(b.String()))
		}
		return communi.NewToolCallResult(toolCallID, b.String())
	case "list":
		rows := t.rt.List()
		if len(rows) == 0 {
			return communi.NewToolCallResult(toolCallID, "(no sub-agents)")
		}
		var b strings.Builder
		for i, row := range rows {
			if i > 0 {
				b.WriteString("\n")
			}
			fmt.Fprintf(&b, "- sub_id=%q agent_id=%s work_dir=%s", row.SubID, row.AgentID, row.WorkDir)
		}
		return communi.NewToolCallResult(toolCallID, b.String())
	case "remove":
		if err := t.rt.Remove(params.SubID); err != nil {
			return communi.ErrorToolCallResult(toolCallID, err)
		}
		return communi.NewToolCallResult(toolCallID, fmt.Sprintf("removed sub-agent %q", strings.TrimSpace(params.SubID)))
	default:
		return communi.ErrorToolCallResult(toolCallID, fmt.Errorf("unknown action %q", params.Action))
	}
}
