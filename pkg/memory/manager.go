package memory

import (
	"context"
	"fmt"
)

// Manager coordinates extraction, storage, and recall across multiple memory types.
type Manager struct {
	store Store
	types map[MemoryType]MemoryTypeHandler
	// defaultTypes used when callers don't specify memory_types.
	defaultTypes []MemoryType
}

func NewManager(store Store, handlers []MemoryTypeHandler) (*Manager, error) {
	if store == nil {
		return nil, fmt.Errorf("memory store is nil")
	}
	types := make(map[MemoryType]MemoryTypeHandler, len(handlers))
	var defaults []MemoryType
	for _, h := range handlers {
		if h == nil {
			continue
		}
		types[h.Type()] = h
		// For now we treat DialogueRaw as the default.
		if h.Type() == DialogueRaw {
			defaults = append(defaults, h.Type())
		}
	}
	if len(defaults) == 0 {
		// Still keep the system usable even if handlers change later.
		defaults = []MemoryType{DialogueRaw}
	}
	return &Manager{
		store:        store,
		types:        types,
		defaultTypes: defaults,
	}, nil
}

func DefaultManager() (*Manager, error) {
	st, err := NewVectorMemoryStore()
	if err != nil {
		return nil, err
	}
	return NewManager(st, []MemoryTypeHandler{
		&dialogueRawHandler{},
	})
}

// WriteDialogue writes the current user+assistant exchange into all registered memory types.
// Currently only DialogueRaw is implemented.
func (m *Manager) WriteDialogue(ctx context.Context, sessionID string, userText string, assistantText string) error {
	if m == nil || m.store == nil {
		return nil
	}
	for _, h := range m.types {
		text, extra, err := h.Build(userText, assistantText)
		if err != nil {
			return err
		}
		md := map[string]any{
			"mem_kind":       "memory",
			"memory_type":    string(h.Type()),
			"session_id":     sessionID,
			"memory_handler": string(h.Type()),
		}
		for k, v := range extra {
			md[k] = v
		}
		// Avoid saving empty session_id so filters can omit it.
		if sessionID == "" {
			delete(md, "session_id")
		}
		_, err = m.store.Upsert(ctx, text, md)
		if err != nil {
			return err
		}
	}
	return nil
}

// Recall searches long-term memories by query and returns matching chunks.
func (m *Manager) Recall(ctx context.Context, query string, sessionID string, memoryTypes []MemoryType, topK int) ([]Chunk, error) {
	if m == nil || m.store == nil {
		return nil, nil
	}
	if query == "" {
		return nil, nil
	}
	if topK <= 0 {
		topK = 8
	}
	if len(memoryTypes) == 0 {
		memoryTypes = m.defaultTypes
	}
	// Build filters for metadata-based isolation (documents vs memory).
	filters := map[string]any{
		"mem_kind":    "memory",
		"memory_type": memoryTypesToStrings(memoryTypes),
	}
	// Per requirement: long-term memory recall does not need session_id isolation.
	return m.store.Search(ctx, query, topK, filters)
}

func memoryTypesToStrings(mts []MemoryType) []string {
	out := make([]string, 0, len(mts))
	for _, mt := range mts {
		if mt == "" {
			continue
		}
		out = append(out, string(mt))
	}
	return out
}
