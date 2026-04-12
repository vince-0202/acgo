package agent_new

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/vince-0202/acgo/pkg/keys"
)

type SubAgentSpec struct {
	ID                 string
	Role               string
	RolePrompt         string
	Capabilities       []string
	AllowedPeers       []string
	Constraints        []string
	InheritModel       bool
	InheritTools       bool
	InheritProjectRoot bool
}

type SubAgentInfo struct {
	ID      string
	AgentID string
	WorkDir string
	Role    string
}

type SubAgentFactory func(ctx context.Context, parent AgentRuntime, spec SubAgentSpec) (*Agent, error)

type SubAgentManager struct {
	parent   *Agent
	mu       sync.Mutex
	children map[string]*Agent
	specs    map[string]SubAgentSpec
	factory  SubAgentFactory
}

func NewSubAgentManager(parent *Agent) *SubAgentManager {
	return &SubAgentManager{
		parent:   parent,
		children: make(map[string]*Agent),
		specs:    make(map[string]SubAgentSpec),
	}
}

func normalizeSubAgentID(id string) string {
	id = strings.TrimSpace(strings.ToLower(id))
	id = strings.ReplaceAll(id, " ", "-")
	return id
}

func (m *SubAgentManager) SetFactory(factory SubAgentFactory) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.factory = factory
}

func (m *SubAgentManager) Register(id string, spec SubAgentSpec, child *Agent) error {
	if m == nil {
		return fmt.Errorf("sub-agent manager is nil")
	}
	key := normalizeSubAgentID(id)
	if key == "" {
		return fmt.Errorf("sub-agent id is required")
	}
	if child == nil {
		return fmt.Errorf("child agent is nil")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.children[key]; exists {
		return fmt.Errorf("sub-agent %q already exists", key)
	}
	spec.ID = key
	m.children[key] = child
	m.specs[key] = spec
	return nil
}

func (m *SubAgentManager) Create(ctx context.Context, spec SubAgentSpec) (string, error) {
	if m == nil {
		return "", fmt.Errorf("sub-agent manager is nil")
	}
	key := normalizeSubAgentID(spec.ID)
	if key == "" {
		return "", fmt.Errorf("sub-agent id is required")
	}

	m.mu.Lock()
	if _, exists := m.children[key]; exists {
		m.mu.Unlock()
		return "", fmt.Errorf("sub-agent %q already exists", key)
	}
	factory := m.factory
	m.mu.Unlock()
	if factory == nil {
		return "", fmt.Errorf("sub-agent factory is not configured")
	}

	spec.ID = key
	child, err := factory(ctx, m.parent, spec)
	if err != nil {
		return "", err
	}
	if err := m.Register(key, spec, child); err != nil {
		return "", err
	}
	return child.ID(), nil
}

func (m *SubAgentManager) Get(id string) (*Agent, bool) {
	if m == nil {
		return nil, false
	}
	key := normalizeSubAgentID(id)
	if key == "" {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	child, ok := m.children[key]
	return child, ok
}

func (m *SubAgentManager) List() []SubAgentInfo {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	keys := make([]string, 0, len(m.children))
	for key := range m.children {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]SubAgentInfo, 0, len(keys))
	for _, key := range keys {
		child := m.children[key]
		spec := m.specs[key]
		workDir := ""
		if child != nil {
			workDir = child.Context().WorkDir()
		}
		out = append(out, SubAgentInfo{
			ID:      key,
			AgentID: child.ID(),
			WorkDir: filepath.Clean(workDir),
			Role:    spec.Role,
		})
	}
	return out
}

func (m *SubAgentManager) Remove(id string) error {
	if m == nil {
		return nil
	}
	key := normalizeSubAgentID(id)
	if key == "" {
		return fmt.Errorf("sub-agent id is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.children[key]; !exists {
		return fmt.Errorf("unknown sub-agent %q", key)
	}
	delete(m.children, key)
	delete(m.specs, key)
	return nil
}

func (m *SubAgentManager) Run(ctx context.Context, id string, prompt string) (string, error) {
	child, ok := m.Get(id)
	if !ok {
		return "", fmt.Errorf("unknown sub-agent %q", id)
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return "", fmt.Errorf("prompt is empty")
	}
	if err := child.Prompt(ctx, prompt); err != nil {
		return "", err
	}
	msgs := child.Context().MessageSnapshot()
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == keys.AgentRoleAssistant {
			return msgs[i].ContentBlocksToText(), nil
		}
	}
	return "", nil
}
