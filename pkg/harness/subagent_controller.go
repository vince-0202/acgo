package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/vince-0202/acgo/pkg/agent"
	"github.com/vince-0202/acgo/pkg/communi"
)

const subAgentToolName = "sub_agent"

type ChildControllerFactory func() Controller

type ChildAgentBuilder interface {
	BuildChild(ctx context.Context, parent agent.AgentRuntime, spec agent.SubAgentSpec) (*agent.Agent, error)
}

type SubAgentControllerOptions struct {
	Builder                  ChildAgentBuilder
	ChildControllerFactories []ChildControllerFactory
	RegisterTool             bool
}

type SubAgentController struct {
	opts           SubAgentControllerOptions
	installedAgent agent.AgentRuntime
	registeredTool agent.Tool
}

func NewSubAgentController(opts SubAgentControllerOptions) *SubAgentController {
	if !opts.RegisterTool {
		opts.RegisterTool = true
	}
	return &SubAgentController{opts: opts}
}

func (c *SubAgentController) Name() string {
	return "subagent"
}

func (c *SubAgentController) Install(runtime agent.AgentRuntime) (func(), error) {
	c.installedAgent = runtime
	builder := c.opts.Builder
	if builder == nil {
		builder = &defaultChildAgentBuilder{controllerFactories: c.opts.ChildControllerFactories}
	}
	runtime.SubAgentManager().SetFactory(func(ctx context.Context, parent agent.AgentRuntime, spec agent.SubAgentSpec) (*agent.Agent, error) {
		return builder.BuildChild(ctx, parent, spec)
	})

	if c.opts.RegisterTool {
		c.registeredTool = newSubAgentTool(runtime.SubAgentManager())
		runtime.ToolManager().RegisterTool(c.registeredTool)
	}

	return func() {
		if c.installedAgent != nil {
			c.installedAgent.SubAgentManager().SetFactory(nil)
			if c.registeredTool != nil {
				c.installedAgent.ToolManager().UnregisterTool(c.registeredTool.Name())
			}
		}
		c.installedAgent = nil
		c.registeredTool = nil
	}, nil
}

type defaultChildAgentBuilder struct {
	controllerFactories []ChildControllerFactory
}

func (b *defaultChildAgentBuilder) BuildChild(ctx context.Context, parent agent.AgentRuntime, spec agent.SubAgentSpec) (*agent.Agent, error) {
	spec.ID = normalizeSubAgentID(spec.ID)
	if spec.ID == "" {
		return nil, fmt.Errorf("sub-agent id is required")
	}

	childWorkDir := filepath.Join(parent.ContextManager().WorkDir(), "subagents", spec.ID)
	childTools := cloneChildTools(parent.ToolManager().RegisteredTools())
	child := agent.New(agent.Options{
		ID:       parent.ID() + "-sub-" + spec.ID,
		WorkDir:  childWorkDir,
		Model:    parent.Model(),
		Provider: parent.Provider(),
		Tools:    childTools,
		InitialState: agent.State{
			WorkDir:       childWorkDir,
			ThinkingLevel: parent.State().ThinkingLevel,
		},
	})
	*child.ContextManager().ContextOptions() = *parent.ContextManager().ContextOptions()
	if spec.InheritProjectRoot || parent.ContextManager().ProjectRoot() != "" {
		_ = child.ContextManager().SetProjectRoot(parent.ContextManager().ProjectRoot())
	}

	factories := b.controllerFactories
	if len(factories) == 0 {
		factories = []ChildControllerFactory{
			func() Controller { return NewContextController() },
		}
	}
	controllers := make([]Controller, 0, len(factories))
	var childContextController *ContextController
	for _, factory := range factories {
		if factory == nil {
			continue
		}
		controller := factory()
		if controller == nil {
			continue
		}
		if cc, ok := controller.(*ContextController); ok && childContextController == nil {
			childContextController = cc
		}
		controllers = append(controllers, controller)
	}
	h := NewHarness(controllers...)
	if err := h.Attach(child); err != nil {
		return nil, err
	}
	rolePrompt := subAgentRolePrompt(spec)
	if rolePrompt == "" {
		rolePrompt = subAgentDefaultBoundaryContract()
	}
	if childContextController != nil {
		childContextController.AppendPersistentPrompt(rolePrompt)
	} else {
		child.ContextManager().AppendPrompt("\n\n" + rolePrompt)
	}
	return child, nil
}

func cloneChildTools(tools []agent.Tool) []agent.Tool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]agent.Tool, 0, len(tools))
	for _, tool := range tools {
		if tool == nil || tool.Name() == subAgentToolName {
			continue
		}
		out = append(out, tool)
	}
	return out
}

type subAgentTool struct {
	rt agent.SubAgentRuntime
}

func newSubAgentTool(rt agent.SubAgentRuntime) agent.Tool {
	return &subAgentTool{rt: rt}
}

func (t *subAgentTool) Name() string {
	return subAgentToolName
}

func (t *subAgentTool) Label() string {
	return "Sub-agent"
}

func (t *subAgentTool) Description() string {
	return "Manage delegated sub-agents: create a worker with explicit role boundaries, run tasks on workers, list them, or remove them."
}

func (t *subAgentTool) JSONSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type": "string",
				"enum": []string{"create", "task", "batch_task", "list", "remove", "dispatch", "message_send", "message_inbox", "message_ack"},
			},
			"sub_id": map[string]any{"type": "string"},
			"prompt": map[string]any{"type": "string"},
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
			"from_sub_id": map[string]any{"type": "string"},
			"to_sub_id":   map[string]any{"type": "string"},
			"payload":     map[string]any{"type": "string"},
			"correlation_id": map[string]any{
				"type": "string",
			},
			"message_id": map[string]any{
				"type": "string",
			},
			"limit": map[string]any{
				"type": "integer",
			},
			"tasks": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"sub_id": map[string]any{"type": "string"},
						"prompt": map[string]any{"type": "string"},
					},
					"required": []string{"sub_id", "prompt"},
				},
			},
			"profile": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"role":          map[string]any{"type": "string"},
					"role_prompt":   map[string]any{"type": "string"},
					"capabilities":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"allowed_peers": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"constraints":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				},
			},
		},
		"required": []string{"action"},
	}
}

func (t *subAgentTool) Execute(ctx context.Context, toolCallID string, args json.RawMessage, update agent.ToolUpdateFunc) communi.ToolCallResult {
	if t == nil || t.rt == nil {
		return communi.ErrorToolCallResult(toolCallID, fmt.Errorf("sub-agent runtime not configured"))
	}
	var params struct {
		Action         string   `json:"action"`
		SubID          string   `json:"sub_id"`
		Prompt         string   `json:"prompt"`
		AutoDispatch   bool     `json:"auto_dispatch"`
		Intent         string   `json:"intent"`
		Constraints    []string `json:"constraints"`
		PreferredSubID []string `json:"preferred_sub_ids"`
		FromSubID      string   `json:"from_sub_id"`
		ToSubID        string   `json:"to_sub_id"`
		Payload        string   `json:"payload"`
		CorrelationID  string   `json:"correlation_id"`
		MessageID      string   `json:"message_id"`
		Limit          int      `json:"limit"`
		Profile        struct {
			Role         string   `json:"role"`
			RolePrompt   string   `json:"role_prompt"`
			Capabilities []string `json:"capabilities"`
			AllowedPeers []string `json:"allowed_peers"`
			Constraints  []string `json:"constraints"`
		} `json:"profile"`
		Tasks []struct {
			SubID  string `json:"sub_id"`
			Prompt string `json:"prompt"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return communi.ErrorToolCallResult(toolCallID, err)
	}

	switch strings.TrimSpace(strings.ToLower(params.Action)) {
	case "create":
		id, err := t.rt.Create(ctx, agent.SubAgentSpec{
			ID:           params.SubID,
			Role:         strings.TrimSpace(params.Profile.Role),
			RolePrompt:   strings.TrimSpace(params.Profile.RolePrompt),
			Capabilities: dedupeStrings(params.Profile.Capabilities),
			AllowedPeers: dedupeStrings(params.Profile.AllowedPeers),
			Constraints:  dedupeStrings(params.Profile.Constraints),
		})
		if err != nil {
			return communi.ErrorToolCallResult(toolCallID, err)
		}
		if child, ok := t.rt.Get(id); ok && child != nil {
			return communi.NewToolCallResult(toolCallID, fmt.Sprintf("created sub-agent: %s (agent_id: %s)", id, child.ID()))
		}
		return communi.NewToolCallResult(toolCallID, "created sub-agent: "+id)
	case "task":
		target := strings.TrimSpace(params.SubID)
		if params.AutoDispatch {
			decision, err := t.rt.DispatchTask(ctx, agent.DispatchRequest{
				Task:            params.Prompt,
				Intent:          params.Intent,
				Constraints:     params.Constraints,
				PreferredSubIDs: params.PreferredSubID,
			})
			if err != nil {
				return communi.ErrorToolCallResult(toolCallID, err)
			}
			target = decision.Target
		}
		out, err := t.rt.Run(ctx, target, params.Prompt)
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
				out, err := t.rt.Run(ctx, task.SubID, task.Prompt)
				rows[i] = row{subID: strings.TrimSpace(task.SubID), output: out, err: err}
			}(i)
		}
		wg.Wait()
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].subID < rows[j].subID })
		var b strings.Builder
		hasErr := false
		for i, row := range rows {
			if i > 0 {
				b.WriteString("\n\n")
			}
			if row.err != nil {
				hasErr = true
				fmt.Fprintf(&b, "[%s] error: %v", row.subID, row.err)
				continue
			}
			fmt.Fprintf(&b, "[%s]\n%s", row.subID, strings.TrimSpace(row.output))
		}
		if hasErr {
			return communi.ErrorToolCallResult(toolCallID, fmt.Errorf("%s", b.String()))
		}
		return communi.NewToolCallResult(toolCallID, b.String())
	case "list":
		infos := t.rt.List()
		if len(infos) == 0 {
			return communi.NewToolCallResult(toolCallID, "(no sub-agents)")
		}
		var b strings.Builder
		for i, info := range infos {
			if i > 0 {
				b.WriteString("\n")
			}
			if strings.TrimSpace(info.Role) == "" {
				fmt.Fprintf(&b, "%s\t%s", info.ID, info.AgentID)
				continue
			}
			fmt.Fprintf(&b, "%s\t%s\t%s", info.ID, info.Role, info.AgentID)
		}
		return communi.NewToolCallResult(toolCallID, b.String())
	case "remove":
		if err := t.rt.Remove(params.SubID); err != nil {
			return communi.ErrorToolCallResult(toolCallID, err)
		}
		return communi.NewToolCallResult(toolCallID, "removed sub-agent "+strings.TrimSpace(params.SubID))
	case "dispatch":
		decision, err := t.rt.DispatchTask(ctx, agent.DispatchRequest{
			Task:            params.Prompt,
			Intent:          params.Intent,
			Constraints:     params.Constraints,
			PreferredSubIDs: params.PreferredSubID,
		})
		if err != nil {
			return communi.ErrorToolCallResult(toolCallID, err)
		}
		body, _ := json.MarshalIndent(decision, "", "  ")
		return communi.NewToolCallResult(toolCallID, string(body))
	case "message_send":
		msg, err := t.rt.SendMessage(params.FromSubID, params.ToSubID, params.Intent, params.Payload, params.CorrelationID, nil)
		if err != nil {
			var msgErr *agent.SubAgentMessageError
			if errors.As(err, &msgErr) && msgErr != nil {
				body, _ := json.MarshalIndent(msgErr, "", "  ")
				return communi.ToolCallResult{
					ToolCallID: toolCallID,
					Content:    []*communi.ContentBlock{communi.NewTextContentBlock(string(body))},
					Error:      err,
					Metadata: map[string]any{
						"type":        "sub_agent_message_error",
						"code":        msgErr.Code,
						"reason":      msgErr.Reason,
						"from_sub_id": msgErr.FromSubID,
						"to_sub_id":   msgErr.ToSubID,
						"intent":      msgErr.Intent,
					},
				}
			}
			return communi.ErrorToolCallResult(toolCallID, err)
		}
		body, _ := json.MarshalIndent(msg, "", "  ")
		return communi.NewToolCallResult(toolCallID, string(body))
	case "message_inbox":
		limit := params.Limit
		if limit <= 0 {
			limit = 20
		}
		msgs := t.rt.PullInbox(params.SubID, limit, params.CorrelationID)
		body, _ := json.MarshalIndent(msgs, "", "  ")
		return communi.NewToolCallResult(toolCallID, string(body))
	case "message_ack":
		msg, err := t.rt.AckMessage(params.MessageID)
		if err != nil {
			return communi.ErrorToolCallResult(toolCallID, err)
		}
		body, _ := json.MarshalIndent(msg, "", "  ")
		return communi.NewToolCallResult(toolCallID, string(body))
	default:
		return communi.ErrorToolCallResult(toolCallID, fmt.Errorf("unsupported action %q", params.Action))
	}
}

func normalizeSubAgentID(id string) string {
	id = strings.TrimSpace(strings.ToLower(id))
	id = strings.ReplaceAll(id, " ", "-")
	return id
}

func dedupeStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func subAgentDefaultBoundaryContract() string {
	return "Delegated worker — responsibility boundaries:\n" +
		"- Only execute work that fits the explicit task your coordinator assigns in each turn. If the ask is broader, ambiguous, or belongs to another worker, say so briefly and stop.\n" +
		"- Do not absorb other sub-agents' duties or duplicate their work unless the coordinator explicitly merges scope.\n" +
		"- Escalate conflicts and unclear ownership to the coordinator instead of guessing."
}

func subAgentRolePrompt(profile agent.SubAgentSpec) string {
	if strings.TrimSpace(profile.Role) == "" &&
		strings.TrimSpace(profile.RolePrompt) == "" &&
		len(profile.Capabilities) == 0 &&
		len(profile.Constraints) == 0 &&
		len(profile.AllowedPeers) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Sub-agent responsibility contract — honor these boundaries strictly. Refuse or escalate anything outside them.\n")
	if role := strings.TrimSpace(profile.Role); role != "" {
		b.WriteString("\n- Role (identity): " + role)
	}
	if rp := strings.TrimSpace(profile.RolePrompt); rp != "" {
		b.WriteString("\n- Role description (what you own): " + rp)
	}
	if len(profile.Capabilities) > 0 {
		b.WriteString("\n- In scope:")
		for _, capability := range profile.Capabilities {
			b.WriteString("\n  - " + capability)
		}
	}
	if len(profile.Constraints) > 0 {
		b.WriteString("\n- Hard limits:")
		for _, constraint := range profile.Constraints {
			b.WriteString("\n  - " + constraint)
		}
	}
	if len(profile.AllowedPeers) > 0 {
		b.WriteString("\n- Allowed peers:")
		for _, peer := range profile.AllowedPeers {
			b.WriteString("\n  - " + peer)
		}
	}
	return b.String()
}
