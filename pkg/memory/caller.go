package memory

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// MemoryCaller recalls memories from a backend for the requested memory types.
type MemoryCaller interface {
	Recall(ctx context.Context, query string, sessionID string, memoryTypes []MemoryType, topK int) ([]Chunk, error)
	DefaultMemoryTypes() []MemoryType
}

// VectorStoreMemoryCaller recalls memories from the configured vector store.
type VectorStoreMemoryCaller struct {
	store        Store
	defaultTypes []MemoryType
}

func NewVectorStoreMemoryCaller() (*VectorStoreMemoryCaller, error) {
	store, err := NewVectorMemoryStore()
	if err != nil {
		return nil, err
	}
	return NewVectorStoreMemoryCallerWithStore(store, []MemoryType{DialogueRaw})
}

func NewVectorStoreMemoryCallerWithStore(store Store, defaultTypes []MemoryType) (*VectorStoreMemoryCaller, error) {
	if store == nil {
		return nil, fmt.Errorf("memory store is nil")
	}
	if len(defaultTypes) == 0 {
		defaultTypes = []MemoryType{DialogueRaw}
	}
	return &VectorStoreMemoryCaller{
		store:        store,
		defaultTypes: append([]MemoryType(nil), defaultTypes...),
	}, nil
}

func (c *VectorStoreMemoryCaller) DefaultMemoryTypes() []MemoryType {
	if c == nil || c.defaultTypes == nil {
		return nil
	}
	return append([]MemoryType(nil), c.defaultTypes...)
}

func (c *VectorStoreMemoryCaller) Recall(ctx context.Context, query string, sessionID string, memoryTypes []MemoryType, topK int) ([]Chunk, error) {
	if c == nil || c.store == nil {
		return nil, nil
	}
	if query == "" {
		return nil, nil
	}
	if topK <= 0 {
		topK = 8
	}
	if len(memoryTypes) == 0 {
		memoryTypes = c.defaultTypes
	}
	filters := map[string]any{
		"mem_kind":    "memory",
		"memory_type": memoryTypesToStrings(memoryTypes),
	}
	// Long-term memory recall does not require session-level isolation.
	return c.store.Search(ctx, query, topK, filters)
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

// FileMemoryCaller recalls memories from a local JSONL file.
type FileMemoryCaller struct {
	path         string
	defaultTypes []MemoryType
}

func NewFileMemoryCaller(path string) *FileMemoryCaller {
	return &FileMemoryCaller{
		path:         strings.TrimSpace(path),
		defaultTypes: []MemoryType{DialogueRaw},
	}
}

func NewFileMemoryCallerWithTypes(path string, defaultTypes []MemoryType) *FileMemoryCaller {
	if len(defaultTypes) == 0 {
		defaultTypes = []MemoryType{DialogueRaw}
	}
	return &FileMemoryCaller{
		path:         strings.TrimSpace(path),
		defaultTypes: append([]MemoryType(nil), defaultTypes...),
	}
}

func (c *FileMemoryCaller) DefaultMemoryTypes() []MemoryType {
	if c == nil || c.defaultTypes == nil {
		return nil
	}
	return append([]MemoryType(nil), c.defaultTypes...)
}

func (c *FileMemoryCaller) Recall(_ context.Context, query string, sessionID string, memoryTypes []MemoryType, topK int) ([]Chunk, error) {
	if c == nil || strings.TrimSpace(c.path) == "" {
		return nil, nil
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if topK <= 0 {
		topK = 8
	}
	if len(memoryTypes) == 0 {
		memoryTypes = c.defaultTypes
	}
	allowedTypes := make(map[string]struct{}, len(memoryTypes))
	for _, mt := range memoryTypes {
		if mt == "" {
			continue
		}
		allowedTypes[string(mt)] = struct{}{}
	}

	f, err := os.Open(c.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	results := make([]Chunk, 0)
	queryTokens := tokenizeRecallText(query)
	for scanner.Scan() {
		var record FileMemoryRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			continue
		}
		recordType := strings.TrimSpace(record.MemoryType)
		if recordType == "" {
			recordType = string(DialogueRaw)
		}
		if len(allowedTypes) > 0 {
			if _, ok := allowedTypes[recordType]; !ok {
				continue
			}
		}
		text := strings.TrimSpace(record.Text)
		if text == "" {
			text = strings.TrimSpace("User: " + record.UserText + "\nAssistant: " + record.AssistantText)
		}
		score := scoreRecallText(query, queryTokens, text)
		if score <= 0 {
			continue
		}
		md := map[string]any{
			"mem_kind":       "memory",
			"memory_type":    recordType,
			"memory_handler": record.MemoryHandler,
		}
		if record.SessionID != "" {
			md["session_id"] = record.SessionID
		}
		for k, v := range record.Metadata {
			md[k] = v
		}
		results = append(results, Chunk{
			Text:     text,
			Score:    score,
			Metadata: md,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	if len(results) > topK {
		results = results[:topK]
	}
	return results, nil
}

func tokenizeRecallText(text string) []string {
	fields := strings.Fields(strings.ToLower(text))
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.Trim(field, " \t\r\n.,!?;:\"'()[]{}")
		if field != "" {
			out = append(out, field)
		}
	}
	return out
}

func scoreRecallText(query string, queryTokens []string, text string) float32 {
	normalizedText := strings.ToLower(text)
	normalizedQuery := strings.ToLower(strings.TrimSpace(query))
	if normalizedQuery != "" && strings.Contains(normalizedText, normalizedQuery) {
		return 1
	}
	if len(queryTokens) == 0 {
		return 0
	}
	var hits float32
	for _, token := range queryTokens {
		if strings.Contains(normalizedText, token) {
			hits++
		}
	}
	if hits == 0 {
		return 0
	}
	return hits / float32(len(queryTokens))
}
