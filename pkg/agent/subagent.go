package agent

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/vince-0202/acgo/pkg/communi"
	"github.com/vince-0202/acgo/pkg/harness"
	"github.com/vince-0202/acgo/pkg/keys"
)

var (
	errInvalidSubID    = errors.New("invalid sub_id")
	errSubAgentExists  = errors.New("sub-agent already exists")
	errUnknownSubAgent = errors.New("unknown sub-agent")
	errEmptyPrompt     = errors.New("prompt is empty")
)

const subAgentToolName = "sub_agent"

// SubAgentController owns child agents spawned by the parent agent.
type SubAgentController struct {
	parent   *Agent
	mu       sync.Mutex
	children map[string]*Agent // logical subID -> child agent
	profiles map[string]harness.SubAgentProfile
	bus      *messageBus
}

// NewSubAgentController constructs a controller; the parent must be fully wired
// (tool controller available for cloning tools into children).
func NewSubAgentController(parent *Agent) *SubAgentController {
	return &SubAgentController{
		parent:   parent,
		children: make(map[string]*Agent),
		profiles: make(map[string]harness.SubAgentProfile),
		bus:      newMessageBus(),
	}
}

var _ harness.SubAgentRuntime = (*SubAgentController)(nil)

// Create registers a new sub-agent with its own work directory and message history.
func (c *SubAgentController) Create(subID string) (string, error) {
	return c.CreateWithOptions(harness.SubAgentCreateOptions{
		SubID: subID,
	})
}

func (c *SubAgentController) CreateWithOptions(opts harness.SubAgentCreateOptions) (string, error) {
	subID := opts.SubID
	key := sanitizeSubAgentKey(subID)
	if key == "" {
		return "", errInvalidSubID
	}
	profile := normalizeSubAgentProfile(opts.Profile)
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.children[key]; ok {
		return "", errSubAgentExists
	}
	childID := c.parent.id + "-sub-" + key
	childWorkDir := filepath.Join(c.parent.state.WorkDir, "subagents", key)

	childTrim := harness.ContextOptions{}
	if c.parent.contextController != nil && c.parent.contextController.Options != nil {
		childTrim = *c.parent.contextController.Options
	}
	var perms harness.PermissionsOptions
	if c.parent != nil && c.parent.permissionCtrl != nil {
		pc := c.parent.permissionCtrl
		perms = harness.PermissionsOptions{
			Mode:        pc.Mode(),
			Rules:       pc.Rules(),
			ConfirmHook: pc.GetConfirmHook(),
		}
	}
	child := New(childID, Options{
		WorkDir:     childWorkDir,
		Provider:    c.parent.Provider,
		Model:       c.parent.Model,
		UseTools:    toolsForChildAgent(c.parent),
		ContextTrim: childTrim,
		Permissions: perms,
		InitialState: State{
			WorkDir:       childWorkDir,
			ThinkingLevel: c.parent.state.ThinkingLevel,
		},
		MemoryWriter:        nil,
		DisableSubAgentTool: true,
	})
	if injected := subAgentRolePrompt(profile); injected != "" {
		child.contextController.AppendSystemPrompt("\n\n" + injected)
	} else {
		child.contextController.AppendSystemPrompt("\n\n" + subAgentDefaultBoundaryContract())
	}
	c.children[key] = child
	c.profiles[key] = profile
	return childID, nil
}

// RunTask runs one Prompt turn on the named sub-agent and returns the last assistant text.
func (c *SubAgentController) RunTask(ctx context.Context, subID, prompt string) (string, error) {
	key := sanitizeSubAgentKey(subID)
	if key == "" {
		return "", errInvalidSubID
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return "", errEmptyPrompt
	}
	c.mu.Lock()
	child, ok := c.children[key]
	c.mu.Unlock()
	if !ok {
		return "", errUnknownSubAgent
	}
	if err := child.Prompt(ctx, prompt); err != nil {
		return "", err
	}
	return lastAssistantText(child.GetMessages()), nil
}

// List returns known sub-agents.
func (c *SubAgentController) List() []harness.SubAgentInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	keys := make([]string, 0, len(c.children))
	for k := range c.children {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]harness.SubAgentInfo, 0, len(keys))
	for _, key := range keys {
		ch := c.children[key]
		profile := c.profiles[key]
		out = append(out, harness.SubAgentInfo{
			SubID:   key,
			AgentID: ch.Id(),
			WorkDir: ch.state.WorkDir,
			Role:    profile.Role,
		})
	}
	return out
}

func (c *SubAgentController) GetProfile(subID string) (harness.SubAgentProfile, bool) {
	key := sanitizeSubAgentKey(subID)
	if key == "" {
		return harness.SubAgentProfile{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	prof, ok := c.profiles[key]
	if !ok {
		return harness.SubAgentProfile{}, false
	}
	return copySubAgentProfile(prof), true
}

// AgentBySubID returns the child agent for a logical sub id, or nil.
func (c *SubAgentController) AgentBySubID(subID string) *Agent {
	key := sanitizeSubAgentKey(subID)
	if key == "" {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.children[key]
}

// Remove drops a sub-agent from this controller (does not unregister from runtime).
// setConfirmHookOnAllChildren updates the permission confirm hook on every child agent
// (used when the TUI sets the hook on the parent after children already exist).
func (c *SubAgentController) setConfirmHookOnAllChildren(hook harness.PermissionConfirmHook) {
	if c == nil {
		return
	}
	c.mu.Lock()
	children := make([]*Agent, 0, len(c.children))
	for _, ch := range c.children {
		children = append(children, ch)
	}
	c.mu.Unlock()
	for _, child := range children {
		child.SetPermissionConfirmHook(hook)
	}
}

func (c *SubAgentController) setPermissionModeOnAllChildren(mode harness.PermissionMode) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	children := make([]*Agent, 0, len(c.children))
	for _, ch := range c.children {
		children = append(children, ch)
	}
	c.mu.Unlock()
	var errs []string
	for _, child := range children {
		if err := child.SetPermissionMode(mode); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("sync permission mode to sub-agents: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (c *SubAgentController) Remove(subID string) error {
	key := sanitizeSubAgentKey(subID)
	if key == "" {
		return errInvalidSubID
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.children[key]; !ok {
		return errUnknownSubAgent
	}
	delete(c.children, key)
	delete(c.profiles, key)
	return nil
}

func (c *SubAgentController) SendMessage(fromSubID, toSubID, intent, payload, correlationID string, metadata map[string]any) (MessageEnvelope, error) {
	fromKey := sanitizeSubAgentKey(fromSubID)
	toKey := sanitizeSubAgentKey(toSubID)
	if fromKey == "" {
		return MessageEnvelope{}, fmt.Errorf("invalid from_sub_id")
	}
	if toKey == "" {
		return MessageEnvelope{}, fmt.Errorf("invalid to_sub_id")
	}

	c.mu.Lock()
	_, fromExists := c.children[fromKey]
	_, toExists := c.children[toKey]
	fromProfile := c.profiles[fromKey]
	toProfile := c.profiles[toKey]
	c.mu.Unlock()
	if !fromExists {
		return MessageEnvelope{}, fmt.Errorf("unknown from sub-agent %q", fromSubID)
	}
	if !toExists {
		return MessageEnvelope{}, fmt.Errorf("unknown to sub-agent %q", toSubID)
	}
	if !isPeerAllowed(fromProfile, toKey) {
		return MessageEnvelope{}, fmt.Errorf("%w: %s -> %s", errMessageRouteDenied, fromKey, toKey)
	}
	if !intentMatchesProfile(intent, toProfile) {
		return MessageEnvelope{}, fmt.Errorf("message intent %q not supported by target %q", strings.TrimSpace(intent), toKey)
	}
	return c.bus.Send(MessageEnvelope{
		FromSubID:     fromKey,
		ToSubID:       toKey,
		Intent:        strings.TrimSpace(intent),
		Payload:       strings.TrimSpace(payload),
		CorrelationID: strings.TrimSpace(correlationID),
		Metadata:      metadata,
	})
}

func (c *SubAgentController) PullInbox(subID string, limit int, correlationID string) []MessageEnvelope {
	key := sanitizeSubAgentKey(subID)
	if key == "" {
		return nil
	}
	return c.bus.Pull(key, limit, correlationID)
}

func (c *SubAgentController) AckMessage(messageID string) (MessageEnvelope, error) {
	return c.bus.Ack(messageID)
}

func (c *SubAgentController) MessagesByCorrelation(correlationID string) []MessageEnvelope {
	return c.bus.ListByCorrelation(correlationID)
}

func (c *SubAgentController) RecentMessages(limit int) []MessageEnvelope {
	return c.bus.Recent(limit)
}

func toolsForChildAgent(parent *Agent) []harness.Tool {
	if parent == nil || parent.toolController == nil {
		return nil
	}
	tools := parent.toolController.Tools()
	out := make([]harness.Tool, 0, len(tools))
	for _, t := range tools {
		if t.Name() == subAgentToolName {
			continue
		}
		out = append(out, t)
	}
	return out
}

func lastAssistantText(msgs []communi.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == keys.AgentRoleAssistant {
			return msgs[i].ContentBlocksToText()
		}
	}
	return ""
}

// subAgentCoordinationAddendum nudges the parent model to delegate substantive work when
// sub-agents are registered. Child agents omit the sub_agent tool, so this returns "" for them.
func (a *Agent) subAgentCoordinationAddendum() string {
	if a == nil || a.toolController == nil || a.subAgentController == nil {
		return ""
	}
	if _, ok := a.toolController.FindTool(subAgentToolName); !ok {
		return ""
	}
	infos := a.subAgentController.List()
	if len(infos) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("You have registered sub-agents. Prefer delegating substantive work to them (sub_agent: task, batch_task, dispatch, or task with auto_dispatch) rather than doing large implementation, exploration, or multi-step tool work yourself.\n")
	b.WriteString("When adding workers, define non-overlapping responsibility boundaries via profile (role, role_prompt, capabilities, constraints, allowed_peers) at create time.\n")
	b.WriteString("Your primary job is coordination: break down the user goal, assign clear tasks to the right sub_id (match roles/capabilities), merge and sanity-check their outputs, and only use direct tools for trivial one-offs or when no sub-agent is appropriate.\n")
	b.WriteString("Registered workers:\n")
	for _, info := range infos {
		role := strings.TrimSpace(info.Role)
		if role != "" {
			fmt.Fprintf(&b, "- sub_id=%q role=%q agent_id=%s\n", info.SubID, role, info.AgentID)
			continue
		}
		fmt.Fprintf(&b, "- sub_id=%q agent_id=%s\n", info.SubID, info.AgentID)
	}
	return strings.TrimSpace(b.String())
}

func sanitizeSubAgentKey(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-' || r == '_':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('-')
		default:
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-_")
	if len(out) > 64 {
		out = out[:64]
	}
	return out
}

func normalizeSubAgentProfile(in harness.SubAgentProfile) harness.SubAgentProfile {
	out := harness.SubAgentProfile{
		Role:       strings.TrimSpace(in.Role),
		RolePrompt: strings.TrimSpace(in.RolePrompt),
	}
	appendUnique := func(dst []string, src []string) []string {
		seen := make(map[string]bool, len(dst))
		for _, it := range dst {
			seen[it] = true
		}
		for _, raw := range src {
			v := strings.TrimSpace(raw)
			if v == "" || seen[v] {
				continue
			}
			seen[v] = true
			dst = append(dst, v)
		}
		return dst
	}
	out.Capabilities = appendUnique(nil, in.Capabilities)
	out.AllowedPeers = appendUnique(nil, in.AllowedPeers)
	out.Constraints = appendUnique(nil, in.Constraints)
	return out
}

func copySubAgentProfile(in harness.SubAgentProfile) harness.SubAgentProfile {
	out := in
	out.Capabilities = append([]string(nil), in.Capabilities...)
	out.AllowedPeers = append([]string(nil), in.AllowedPeers...)
	out.Constraints = append([]string(nil), in.Constraints...)
	return out
}

func subAgentDefaultBoundaryContract() string {
	return "Delegated worker — responsibility boundaries:\n" +
		"- Only execute work that fits the explicit task your coordinator assigns in each turn. If the ask is broader, ambiguous, or belongs to another worker, say so briefly and stop.\n" +
		"- Do not absorb other sub-agents' duties or duplicate their work unless the coordinator explicitly merges scope.\n" +
		"- Escalate conflicts and unclear ownership to the coordinator instead of guessing."
}

func subAgentRolePrompt(profile harness.SubAgentProfile) string {
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
		b.WriteString("\n- In scope (you may do): tasks that match these capability tags or the coordinator's scoped prompt:")
		for _, c := range profile.Capabilities {
			b.WriteString("\n  - " + c)
		}
	}
	if len(profile.Constraints) > 0 {
		b.WriteString("\n- Hard limits (must not violate):")
		for _, c := range profile.Constraints {
			b.WriteString("\n  - " + c)
		}
	}
	if len(profile.AllowedPeers) > 0 {
		b.WriteString("\n- Collaboration boundary: you may message only these peer sub_ids (via coordinator tools), not others:")
		for _, p := range profile.AllowedPeers {
			b.WriteString("\n  - " + strings.TrimSpace(p))
		}
	}
	b.WriteString("\n\nIf a request crosses into another worker's domain or breaks a constraint, decline with a one-line reason and suggest which role should handle it.")
	return b.String()
}

func isPeerAllowed(profile harness.SubAgentProfile, toSubID string) bool {
	if len(profile.AllowedPeers) == 0 {
		return true
	}
	for _, p := range profile.AllowedPeers {
		if sanitizeSubAgentKey(p) == toSubID {
			return true
		}
	}
	return false
}

func intentMatchesProfile(intent string, profile harness.SubAgentProfile) bool {
	intent = strings.TrimSpace(strings.ToLower(intent))
	if intent == "" || len(profile.Capabilities) == 0 {
		return true
	}
	for _, c := range profile.Capabilities {
		if strings.ToLower(strings.TrimSpace(c)) == intent {
			return true
		}
	}
	return false
}
