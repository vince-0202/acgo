package agent

import (
	"context"
	"errors"
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
}

// NewSubAgentController constructs a controller; the parent must be fully wired
// (tool controller available for cloning tools into children).
func NewSubAgentController(parent *Agent) *SubAgentController {
	return &SubAgentController{
		parent:   parent,
		children: make(map[string]*Agent),
	}
}

var _ harness.SubAgentRuntime = (*SubAgentController)(nil)

// Create registers a new sub-agent with its own work directory and message history.
func (c *SubAgentController) Create(subID string) (string, error) {
	key := sanitizeSubAgentKey(subID)
	if key == "" {
		return "", errInvalidSubID
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.children[key]; ok {
		return "", errSubAgentExists
	}
	childID := c.parent.id + "-sub-" + key
	childWorkDir := filepath.Join(c.parent.state.WorkDir, "subagents", key)

	child := New(childID, Options{
		WorkDir:  childWorkDir,
		Provider: c.parent.Provider,
		Model:    c.parent.Model,
		UseTools: toolsForChildAgent(c.parent),
		InitialState: State{
			WorkDir:       childWorkDir,
			ThinkingLevel: c.parent.state.ThinkingLevel,
		},
		MemoryWriter:        nil,
		DisableSubAgentTool: true,
	})
	c.children[key] = child
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
		out = append(out, harness.SubAgentInfo{
			SubID:   key,
			AgentID: ch.Id(),
			WorkDir: ch.state.WorkDir,
		})
	}
	return out
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
	return nil
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
