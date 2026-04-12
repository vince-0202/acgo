package agent

import (
	"context"
	"encoding/json"
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
	bus      *messageBus
	factory  SubAgentFactory
}

func NewSubAgentManager(parent *Agent) *SubAgentManager {
	return &SubAgentManager{
		parent:   parent,
		children: make(map[string]*Agent),
		specs:    make(map[string]SubAgentSpec),
		bus:      newMessageBus(),
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
	spec = normalizeSubAgentSpec(spec, key)
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
	// Return logical sub-agent id (manager key), so callers can use it in Get/Run/Remove.
	return key, nil
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

func (m *SubAgentManager) SendMessage(fromSubID, toSubID, intent, payload, correlationID string, metadata map[string]any) (MessageEnvelope, error) {
	if m == nil {
		return MessageEnvelope{}, fmt.Errorf("sub-agent manager is nil")
	}
	fromKey := normalizeSubAgentID(fromSubID)
	toKey := normalizeSubAgentID(toSubID)
	if fromKey == "" {
		return MessageEnvelope{}, &SubAgentMessageError{
			Code:      "invalid_from_sub_id",
			Reason:    "invalid from_sub_id",
			FromSubID: strings.TrimSpace(fromSubID),
			ToSubID:   strings.TrimSpace(toSubID),
			Intent:    strings.TrimSpace(intent),
		}
	}
	if toKey == "" {
		return MessageEnvelope{}, &SubAgentMessageError{
			Code:      "invalid_to_sub_id",
			Reason:    "invalid to_sub_id",
			FromSubID: fromKey,
			ToSubID:   strings.TrimSpace(toSubID),
			Intent:    strings.TrimSpace(intent),
		}
	}

	m.mu.Lock()
	_, fromExists := m.children[fromKey]
	_, toExists := m.children[toKey]
	fromSpec := m.specs[fromKey]
	toSpec := m.specs[toKey]
	m.mu.Unlock()
	if !fromExists {
		return MessageEnvelope{}, &SubAgentMessageError{
			Code:      "unknown_from_sub_agent",
			Reason:    "unknown from sub-agent",
			FromSubID: fromKey,
			ToSubID:   toKey,
			Intent:    strings.TrimSpace(intent),
		}
	}
	if !toExists {
		return MessageEnvelope{}, &SubAgentMessageError{
			Code:      "unknown_to_sub_agent",
			Reason:    "unknown to sub-agent",
			FromSubID: fromKey,
			ToSubID:   toKey,
			Intent:    strings.TrimSpace(intent),
		}
	}
	if !isPeerAllowed(fromSpec, toKey) {
		return MessageEnvelope{}, &SubAgentMessageError{
			Code:      "route_denied",
			Reason:    "peer route denied by allowed_peers",
			FromSubID: fromKey,
			ToSubID:   toKey,
			Intent:    strings.TrimSpace(intent),
		}
	}
	if !intentMatchesSpec(intent, toSpec) {
		return MessageEnvelope{}, &SubAgentMessageError{
			Code:      "intent_not_supported",
			Reason:    "message intent not supported by target capabilities",
			FromSubID: fromKey,
			ToSubID:   toKey,
			Intent:    strings.TrimSpace(intent),
		}
	}

	return m.bus.Send(MessageEnvelope{
		FromSubID:     fromKey,
		ToSubID:       toKey,
		Intent:        strings.TrimSpace(intent),
		Payload:       strings.TrimSpace(payload),
		CorrelationID: strings.TrimSpace(correlationID),
		Metadata:      metadata,
	})
}

type SubAgentMessageError struct {
	Code      string `json:"code"`
	Reason    string `json:"reason"`
	FromSubID string `json:"from_sub_id,omitempty"`
	ToSubID   string `json:"to_sub_id,omitempty"`
	Intent    string `json:"intent,omitempty"`
}

func (e *SubAgentMessageError) Error() string {
	if e == nil {
		return ""
	}
	body, err := json.Marshal(e)
	if err != nil {
		return e.Reason
	}
	return string(body)
}

func (m *SubAgentManager) PullInbox(subID string, limit int, correlationID string) []MessageEnvelope {
	if m == nil {
		return nil
	}
	key := normalizeSubAgentID(subID)
	if key == "" {
		return nil
	}
	return m.bus.Pull(key, limit, correlationID)
}

func (m *SubAgentManager) AckMessage(messageID string) (MessageEnvelope, error) {
	if m == nil {
		return MessageEnvelope{}, fmt.Errorf("sub-agent manager is nil")
	}
	return m.bus.Ack(messageID)
}

func normalizeSubAgentSpec(spec SubAgentSpec, id string) SubAgentSpec {
	spec.ID = normalizeSubAgentID(id)
	spec.Role = strings.TrimSpace(spec.Role)
	spec.RolePrompt = strings.TrimSpace(spec.RolePrompt)
	spec.Capabilities = dedupeNormalizedStrings(spec.Capabilities, false)
	spec.Constraints = dedupeNormalizedStrings(spec.Constraints, false)
	spec.AllowedPeers = dedupeNormalizedStrings(spec.AllowedPeers, true)
	return spec
}

func dedupeNormalizedStrings(items []string, normalizeSubID bool) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if normalizeSubID {
			item = normalizeSubAgentID(item)
		}
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

func isPeerAllowed(spec SubAgentSpec, toSubID string) bool {
	if len(spec.AllowedPeers) == 0 {
		return true
	}
	for _, peer := range spec.AllowedPeers {
		if normalizeSubAgentID(peer) == toSubID {
			return true
		}
	}
	return false
}

func intentMatchesSpec(intent string, spec SubAgentSpec) bool {
	intent = strings.TrimSpace(strings.ToLower(intent))
	if intent == "" || len(spec.Capabilities) == 0 {
		return true
	}
	for _, capability := range spec.Capabilities {
		if strings.ToLower(strings.TrimSpace(capability)) == intent {
			return true
		}
	}
	return false
}
