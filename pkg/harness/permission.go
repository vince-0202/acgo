package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/vince-0202/acgo/pkg/agent"
	"github.com/vince-0202/acgo/pkg/communi"
)

type PermissionMode string

const (
	PermissionModeDefault     PermissionMode = "default"
	PermissionModeAcceptEdits PermissionMode = "acceptEdits"
	PermissionModePlan        PermissionMode = "plan"
	PermissionModeAuto        PermissionMode = "auto"
	PermissionModeBypass      PermissionMode = "bypassPermissions"
)

type PermissionDecision string

const (
	PermissionDecisionAllow       PermissionDecision = "allow"
	PermissionDecisionDeny        PermissionDecision = "deny"
	PermissionDecisionNeedConfirm PermissionDecision = "need_confirm"
)

type PermissionEffect string

const (
	PermissionEffectAllow   PermissionEffect = "allow"
	PermissionEffectDeny    PermissionEffect = "deny"
	PermissionEffectConfirm PermissionEffect = "confirm"
)

type PermissionRequest struct {
	Action   string
	Resource string
	Metadata map[string]any
}

type PermissionRule struct {
	Action   string
	Resource string
	Effect   PermissionEffect
}

type PermissionResult struct {
	Decision PermissionDecision
	Reason   string
}

type PermissionConfirmHook func(ctx context.Context, req PermissionRequest) (approved bool, reason string, err error)

type PermissionController struct {
	mode        PermissionMode
	rules       []PermissionRule
	confirmHook PermissionConfirmHook
	agent       agent.AgentRuntime
}

type PermissionsOptions struct {
	Mode        PermissionMode
	Rules       []PermissionRule
	ConfirmHook PermissionConfirmHook
}

func NewPermissionController(mode PermissionMode, rules []PermissionRule, hook PermissionConfirmHook) *PermissionController {
	mode = normalizePermissionMode(mode)
	copied := make([]PermissionRule, 0, len(rules))
	copied = append(copied, rules...)
	return &PermissionController{
		mode:        mode,
		rules:       copied,
		confirmHook: hook,
	}
}

func (pc *PermissionController) Name() string {
	return "permission"
}

func (pc *PermissionController) Clone() *PermissionController {
	if pc == nil {
		return nil
	}
	return NewPermissionController(pc.Mode(), pc.Rules(), pc.GetConfirmHook())
}

func (pc *PermissionController) Install(runtime agent.AgentRuntime) (func(), error) {
	pc.agent = runtime
	pc.syncPlanMode()
	uninstall := runtime.ToolManager().RegisterMiddleware(func(ctx context.Context, req agent.ToolExecutionRequest, next agent.ToolExecutionHandler) (communi.ToolCallResult, error) {
		toolArgs := strings.TrimSpace(string(req.Args))
		workDir := runtime.ContextManager().ToolWorkingDirectory()
		_, err := pc.Check(ctx, PermissionRequest{
			Action:   "tool.execute",
			Resource: "tool:" + req.Tool.Name(),
			Metadata: map[string]any{
				"tool_call_id": req.ToolCall.ID,
				"tool_name":    req.Tool.Name(),
				"tool_args":    toolArgs,
				"workdir":      workDir,
			},
		})
		if err != nil {
			res := communi.ErrorToolCallResult(req.ToolCall.ID, err)
			return res, err
		}
		return next(ctx, req)
	})
	return func() {
		uninstall()
		if pc.agent == runtime {
			pc.agent = nil
		}
	}, nil
}

func (pc *PermissionController) SetConfirmHook(hook PermissionConfirmHook) {
	pc.confirmHook = hook
}

func normalizePermissionMode(mode PermissionMode) PermissionMode {
	m := strings.TrimSpace(string(mode))
	if m == "" {
		return PermissionModeDefault
	}
	return PermissionMode(m)
}

func ValidatePermissionMode(mode PermissionMode) error {
	switch normalizePermissionMode(mode) {
	case PermissionModeDefault, PermissionModeAcceptEdits, PermissionModePlan, PermissionModeAuto, PermissionModeBypass:
		return nil
	default:
		return fmt.Errorf("invalid permission mode: %q", mode)
	}
}

func (pc *PermissionController) SetMode(mode PermissionMode) error {
	if pc == nil {
		return nil
	}
	mode = normalizePermissionMode(mode)
	if err := ValidatePermissionMode(mode); err != nil {
		return err
	}
	pc.mode = mode
	pc.syncPlanMode()
	return nil
}

func (pc *PermissionController) syncPlanMode() {
	if pc == nil || pc.agent == nil {
		return
	}
	setter, ok := pc.agent.(interface{ SetPlanMode(bool) })
	if !ok {
		return
	}
	setter.SetPlanMode(pc.Mode() == PermissionModePlan)
}

// GetConfirmHook returns the current UI confirmation callback, if any.
func (pc *PermissionController) GetConfirmHook() PermissionConfirmHook {
	if pc == nil {
		return nil
	}
	return pc.confirmHook
}

// Rules returns a copy of configured permission rules.
func (pc *PermissionController) Rules() []PermissionRule {
	if pc == nil {
		return nil
	}
	out := make([]PermissionRule, len(pc.rules))
	copy(out, pc.rules)
	return out
}

func (pc *PermissionController) Check(ctx context.Context, req PermissionRequest) (PermissionResult, error) {
	action := strings.TrimSpace(req.Action)
	resource := strings.TrimSpace(req.Resource)
	if action == "" || resource == "" {
		return PermissionResult{}, &PermissionError{
			Mode:     pc.Mode(),
			Action:   action,
			Resource: resource,
			Decision: PermissionDecisionDeny,
			Reason:   "permission request requires non-empty action and resource",
		}
	}

	decision, reason := pc.evaluate(req)
	if decision != PermissionDecisionNeedConfirm {
		if decision == PermissionDecisionDeny {
			return PermissionResult{}, &PermissionError{
				Mode:     pc.Mode(),
				Action:   action,
				Resource: resource,
				Decision: decision,
				Reason:   reason,
			}
		}
		return PermissionResult{Decision: decision, Reason: reason}, nil
	}

	if pc.confirmHook == nil {
		return PermissionResult{}, &PermissionError{
			Mode:     pc.Mode(),
			Action:   action,
			Resource: resource,
			Decision: decision,
			Reason:   "permission requires confirmation but no confirm hook configured",
		}
	}
	approved, hookReason, err := pc.confirmHook(ctx, req)
	if err != nil {
		return PermissionResult{}, &PermissionError{
			Mode:     pc.Mode(),
			Action:   action,
			Resource: resource,
			Decision: decision,
			Reason:   "confirm hook failed: " + err.Error(),
		}
	}
	if !approved {
		if strings.TrimSpace(hookReason) == "" {
			hookReason = "request rejected by confirmation hook"
		}
		return PermissionResult{}, &PermissionError{
			Mode:     pc.Mode(),
			Action:   action,
			Resource: resource,
			Decision: PermissionDecisionDeny,
			Reason:   hookReason,
		}
	}
	if strings.TrimSpace(hookReason) == "" {
		hookReason = "request approved by confirmation hook"
	}
	return PermissionResult{Decision: PermissionDecisionAllow, Reason: hookReason}, nil
}

func (pc *PermissionController) Mode() PermissionMode {
	if pc == nil {
		return PermissionModeDefault
	}
	return normalizePermissionMode(pc.mode)
}

func (pc *PermissionController) evaluate(req PermissionRequest) (PermissionDecision, string) {
	mode := pc.Mode()
	if mode == PermissionModeBypass {
		if shouldGuardProtectedWrite(req) {
			return PermissionDecisionNeedConfirm, "protected directory write requires confirmation in bypassPermissions mode"
		}
		return PermissionDecisionAllow, "default allow in bypassPermissions mode"
	}
	if matched, ok := pc.firstMatch(req); ok {
		switch matched.Effect {
		case PermissionEffectAllow:
			return PermissionDecisionAllow, "matched allow rule"
		case PermissionEffectDeny:
			return PermissionDecisionDeny, "matched deny rule"
		case PermissionEffectConfirm:
			return PermissionDecisionNeedConfirm, "matched confirm rule"
		}
	}

	if strings.TrimSpace(req.Action) == "tool.execute" {
		toolName := permissionToolName(req)
		if shouldGuardProtectedWrite(req) {
			return PermissionDecisionNeedConfirm, "protected directory write requires confirmation"
		}
		switch pc.Mode() {
		case PermissionModePlan:
			switch toolPermissionClass(toolName) {
			case permissionClassReadOnly:
				return PermissionDecisionAllow, "default allow read-only tool in plan mode"
			case permissionClassWrite:
				return PermissionDecisionDeny, "default deny file-edit tool in plan mode"
			default:
				return PermissionDecisionNeedConfirm, "default confirm command/external tool in plan mode"
			}
		case PermissionModeAcceptEdits:
			switch toolPermissionClass(toolName) {
			case permissionClassReadOnly, permissionClassWrite:
				return PermissionDecisionAllow, "default allow read/write tool in acceptEdits mode"
			default:
				return PermissionDecisionNeedConfirm, "default confirm command/external tool in acceptEdits mode"
			}
		case PermissionModeAuto:
			return PermissionDecisionAllow, "default allow in auto mode"
		default:
			switch toolPermissionClass(toolName) {
			case permissionClassReadOnly:
				return PermissionDecisionAllow, "default allow read-only tool in default mode"
			default:
				return PermissionDecisionNeedConfirm, "default confirm non-read tool in default mode"
			}
		}
	}

	// Non-tool actions keep conservative behavior.
	return PermissionDecisionNeedConfirm, "default confirm for non-tool action"
}

const (
	permissionClassReadOnly = "read_only"
	permissionClassWrite    = "write"
	permissionClassOther    = "other"
)

func permissionToolName(req PermissionRequest) string {
	if req.Metadata != nil {
		if v, ok := req.Metadata["tool_name"].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	resource := strings.TrimSpace(req.Resource)
	if strings.HasPrefix(resource, "tool:") {
		return strings.TrimSpace(strings.TrimPrefix(resource, "tool:"))
	}
	return resource
}

func toolPermissionClass(toolName string) string {
	switch strings.TrimSpace(toolName) {
	case "read", "list", "grep", "memory_recall", "rag_search":
		return permissionClassReadOnly
	case "write", "edit":
		return permissionClassWrite
	default:
		return permissionClassOther
	}
}

func shouldGuardProtectedWrite(req PermissionRequest) bool {
	if strings.TrimSpace(req.Action) != "tool.execute" {
		return false
	}
	toolName := permissionToolName(req)
	if toolPermissionClass(toolName) != permissionClassWrite {
		return false
	}
	args := ""
	if req.Metadata != nil {
		if v, ok := req.Metadata["tool_args"].(string); ok {
			args = strings.TrimSpace(v)
		}
	}
	if args == "" {
		return false
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(args), &payload); err != nil {
		return false
	}
	rawPath, _ := payload["path"].(string)
	return isProtectedPath(rawPath)
}

func isProtectedPath(raw string) bool {
	p := strings.TrimSpace(raw)
	if p == "" {
		return false
	}
	p = filepath.ToSlash(filepath.Clean(p))
	p = strings.TrimPrefix(p, "./")
	parts := strings.Split(p, "/")
	for i := 0; i < len(parts); i++ {
		if parts[i] == "" {
			continue
		}
		rest := strings.Join(parts[i:], "/")
		switch {
		case rest == ".git" || strings.HasPrefix(rest, ".git/"):
			return true
		case rest == ".vscode" || strings.HasPrefix(rest, ".vscode/"):
			return true
		case rest == ".idea" || strings.HasPrefix(rest, ".idea/"):
			return true
		case rest == ".husky" || strings.HasPrefix(rest, ".husky/"):
			return true
		case rest == ".claude" || strings.HasPrefix(rest, ".claude/"):
			// Claude exceptions.
			if rest == ".claude/commands" || strings.HasPrefix(rest, ".claude/commands/") {
				return false
			}
			if rest == ".claude/agents" || strings.HasPrefix(rest, ".claude/agents/") {
				return false
			}
			if rest == ".claude/skills" || strings.HasPrefix(rest, ".claude/skills/") {
				return false
			}
			return true
		}
	}
	return false
}

func (pc *PermissionController) firstMatch(req PermissionRequest) (PermissionRule, bool) {
	for _, r := range pc.rules {
		if matchField(r.Action, req.Action) && matchField(r.Resource, req.Resource) {
			return r, true
		}
	}
	return PermissionRule{}, false
}

func matchField(ruleValue, target string) bool {
	r := strings.TrimSpace(ruleValue)
	if r == "" || r == "*" {
		return true
	}
	return r == target
}

type PermissionError struct {
	Mode     PermissionMode
	Action   string
	Resource string
	Decision PermissionDecision
	Reason   string
}

func (e *PermissionError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("permission denied (mode=%s action=%s resource=%s decision=%s): %s",
		e.Mode, e.Action, e.Resource, e.Decision, e.Reason)
}
