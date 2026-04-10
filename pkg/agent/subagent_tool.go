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
		"On create, always define clear responsibility boundaries via profile (role, role_prompt, capabilities=in-scope work, constraints=hard limits, allowed_peers=who may collaborate) so workers do not overlap or overreach. " +
		"When sub-agents exist, the caller should usually act as coordinator—delegate substantive coding and exploration to workers (task, batch_task, dispatch, message_send) and reserve direct tools for light coordination or when no worker fits. " +
		"When the model requests multiple tool calls in one turn, those runs execute in parallel. " +
		"Sub-agents share the same model and tools as this agent but have separate conversation state and workspace under subagents/<sub_id>."
}

func (t *subAgentTool) JSONSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"create", "task", "batch_task", "list", "remove", "dispatch", "message_send", "message_inbox", "message_ack"},
				"description": "create: register a sub-agent — supply profile so each worker has explicit responsibility boundaries (no overlapping ownership); task: run one prompt on one sub-agent; batch_task: concurrent tasks; list; remove; dispatch; message_*",
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
			"profile": map[string]any{
				"type":        "object",
				"description": "Responsibility boundaries for this sub_id. Use distinct roles and non-overlapping capabilities across workers; constraints are hard refusals; allowed_peers limits cross-talk.",
				"properties": map[string]any{
					"role": map[string]any{
						"type":        "string",
						"description": "Short role name (e.g. reviewer, implementer). Must not duplicate another worker's mandate.",
					},
					"role_prompt": map[string]any{
						"type":        "string",
						"description": "What this worker owns end-to-end vs what is explicitly out of scope for others to avoid.",
					},
					"capabilities": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "In-scope intent tags (e.g. implement, review). Dispatch and message routing may match these; keep disjoint across workers when possible.",
					},
					"allowed_peers": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "sub_ids this worker may message; empty means no peer restriction in profile (runtime may still enforce).",
					},
					"constraints": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Hard limits (e.g. must not edit production config). Worker must refuse violations.",
					},
				},
			},
			"auto_dispatch": map[string]any{
				"type":        "boolean",
				"description": "When action=task and auto_dispatch=true, dispatcher selects target sub-agent.",
			},
			"intent": map[string]any{
				"type":        "string",
				"description": "Semantic task intent used by dispatch and message routing.",
			},
			"constraints": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
			"preferred_sub_ids": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
			"from_sub_id": map[string]any{
				"type": "string",
			},
			"to_sub_id": map[string]any{
				"type": "string",
			},
			"payload": map[string]any{
				"type": "string",
			},
			"correlation_id": map[string]any{
				"type": "string",
			},
			"message_id": map[string]any{
				"type": "string",
			},
			"limit": map[string]any{
				"type": "integer",
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
		Action         string                  `json:"action"`
		SubID          string                  `json:"sub_id"`
		Prompt         string                  `json:"prompt"`
		Profile        harness.SubAgentProfile `json:"profile"`
		AutoDispatch   bool                    `json:"auto_dispatch"`
		Intent         string                  `json:"intent"`
		Constraints    []string                `json:"constraints"`
		PreferredSubID []string                `json:"preferred_sub_ids"`
		FromSubID      string                  `json:"from_sub_id"`
		ToSubID        string                  `json:"to_sub_id"`
		Payload        string                  `json:"payload"`
		CorrelationID  string                  `json:"correlation_id"`
		MessageID      string                  `json:"message_id"`
		Limit          int                     `json:"limit"`
		Tasks          []struct {
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
		id, err := t.rt.CreateWithOptions(harness.SubAgentCreateOptions{
			SubID:   params.SubID,
			Profile: params.Profile,
		})
		if err != nil {
			return communi.ErrorToolCallResult(toolCallID, err)
		}
		msg := fmt.Sprintf("created sub-agent %q as agent id %s.", strings.TrimSpace(params.SubID), id)
		msg += "\nKeep responsibility boundaries explicit: assign tasks that fit profile.role/capabilities; use profile.constraints for refusals; avoid giving this sub_id work owned by another worker."
		if !subAgentProfileSpecifiesBoundaries(params.Profile) {
			msg += "\nNo profile was provided—either recreate with profile (recommended) or keep every task prompt tightly scoped so boundaries stay clear."
		}
		return communi.NewToolCallResult(toolCallID, msg)
	case "task":
		subID := strings.TrimSpace(params.SubID)
		if params.AutoDispatch {
			dispatcher, ok := t.rt.(interface {
				DispatchTask(context.Context, DispatchRequest) (DispatchDecision, error)
			})
			if !ok {
				return communi.ErrorToolCallResult(toolCallID, fmt.Errorf("dispatch runtime not supported"))
			}
			d, err := dispatcher.DispatchTask(ctx, DispatchRequest{
				Task:            params.Prompt,
				Intent:          params.Intent,
				Constraints:     params.Constraints,
				PreferredSubIDs: params.PreferredSubID,
			})
			if err != nil {
				return communi.ErrorToolCallResult(toolCallID, err)
			}
			subID = d.Target
			if update != nil {
				update(harness.ToolUpdate{
					Text:     fmt.Sprintf("dispatch selected sub-agent %q (%s)", d.Target, strings.TrimSpace(d.Reason)),
					Progress: 0.2,
				})
			}
		}
		out, err := t.rt.RunTask(ctx, subID, params.Prompt)
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
			role := strings.TrimSpace(row.Role)
			if role == "" {
				role = "-"
			}
			fmt.Fprintf(&b, "- sub_id=%q agent_id=%s role=%q work_dir=%s", row.SubID, row.AgentID, role, row.WorkDir)
		}
		return communi.NewToolCallResult(toolCallID, b.String())
	case "remove":
		if err := t.rt.Remove(params.SubID); err != nil {
			return communi.ErrorToolCallResult(toolCallID, err)
		}
		return communi.NewToolCallResult(toolCallID, fmt.Sprintf("removed sub-agent %q", strings.TrimSpace(params.SubID)))
	case "dispatch":
		dispatcher, ok := t.rt.(interface {
			DispatchTask(context.Context, DispatchRequest) (DispatchDecision, error)
		})
		if !ok {
			return communi.ErrorToolCallResult(toolCallID, fmt.Errorf("dispatch runtime not supported"))
		}
		d, err := dispatcher.DispatchTask(ctx, DispatchRequest{
			Task:            params.Prompt,
			Intent:          params.Intent,
			Constraints:     params.Constraints,
			PreferredSubIDs: params.PreferredSubID,
		})
		if err != nil {
			return communi.ErrorToolCallResult(toolCallID, err)
		}
		body, _ := json.MarshalIndent(d, "", "  ")
		return communi.NewToolCallResult(toolCallID, string(body))
	case "message_send":
		sender, ok := t.rt.(interface {
			SendMessage(fromSubID, toSubID, intent, payload, correlationID string, metadata map[string]any) (MessageEnvelope, error)
		})
		if !ok {
			return communi.ErrorToolCallResult(toolCallID, fmt.Errorf("message bus runtime not supported"))
		}
		msg, err := sender.SendMessage(params.FromSubID, params.ToSubID, params.Intent, params.Payload, params.CorrelationID, nil)
		if err != nil {
			return communi.ErrorToolCallResult(toolCallID, err)
		}
		body, _ := json.MarshalIndent(msg, "", "  ")
		return communi.NewToolCallResult(toolCallID, string(body))
	case "message_inbox":
		inbox, ok := t.rt.(interface {
			PullInbox(subID string, limit int, correlationID string) []MessageEnvelope
		})
		if !ok {
			return communi.ErrorToolCallResult(toolCallID, fmt.Errorf("message bus runtime not supported"))
		}
		limit := params.Limit
		if limit <= 0 {
			limit = 20
		}
		msgs := inbox.PullInbox(params.SubID, limit, params.CorrelationID)
		body, _ := json.MarshalIndent(msgs, "", "  ")
		return communi.NewToolCallResult(toolCallID, string(body))
	case "message_ack":
		acker, ok := t.rt.(interface {
			AckMessage(messageID string) (MessageEnvelope, error)
		})
		if !ok {
			return communi.ErrorToolCallResult(toolCallID, fmt.Errorf("message bus runtime not supported"))
		}
		msg, err := acker.AckMessage(params.MessageID)
		if err != nil {
			return communi.ErrorToolCallResult(toolCallID, err)
		}
		body, _ := json.MarshalIndent(msg, "", "  ")
		return communi.NewToolCallResult(toolCallID, string(body))
	default:
		return communi.ErrorToolCallResult(toolCallID, fmt.Errorf("unknown action %q", params.Action))
	}
}

func subAgentProfileSpecifiesBoundaries(p harness.SubAgentProfile) bool {
	return strings.TrimSpace(p.Role) != "" ||
		strings.TrimSpace(p.RolePrompt) != "" ||
		len(p.Capabilities) > 0 ||
		len(p.Constraints) > 0 ||
		len(p.AllowedPeers) > 0
}
