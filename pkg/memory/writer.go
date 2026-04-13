package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// MemoryWriter persists dialogue memories to a specific backend.
type MemoryWriter interface {
	WriteDialogue(ctx context.Context, sessionID string, userText string, assistantText string) error
}

// VectorStoreMemoryWriter writes dialogue memories into the configured vector store.
type VectorStoreMemoryWriter struct {
	store    Store
	types    map[MemoryType]MemoryTypeHandler
	typeList []MemoryType
}

func NewVectorStoreMemoryWriter() (*VectorStoreMemoryWriter, error) {
	store, err := NewVectorMemoryStore()
	if err != nil {
		return nil, err
	}
	return NewVectorStoreMemoryWriterWithStore(store, []MemoryTypeHandler{
		&dialogueRawHandler{},
	})
}

func NewVectorStoreMemoryWriterWithStore(store Store, handlers []MemoryTypeHandler) (*VectorStoreMemoryWriter, error) {
	if store == nil {
		return nil, fmt.Errorf("memory store is nil")
	}
	types, typeList, err := registerMemoryHandlers(handlers)
	if err != nil {
		return nil, err
	}
	return &VectorStoreMemoryWriter{
		store:    store,
		types:    types,
		typeList: typeList,
	}, nil
}

func (w *VectorStoreMemoryWriter) WriteDialogue(ctx context.Context, sessionID string, userText string, assistantText string) error {
	if w == nil || w.store == nil {
		return nil
	}
	for _, mt := range w.typeList {
		handler := w.types[mt]
		if handler == nil {
			continue
		}
		text, extra, err := handler.Build(userText, assistantText)
		if err != nil {
			return err
		}
		md := map[string]any{
			"mem_kind":       "memory",
			"memory_type":    string(handler.Type()),
			"session_id":     sessionID,
			"memory_handler": string(handler.Type()),
		}
		for k, v := range extra {
			md[k] = v
		}
		if sessionID == "" {
			delete(md, "session_id")
		}
		if _, err := w.store.Upsert(ctx, text, md); err != nil {
			return err
		}
	}
	return nil
}

// FileMemoryWriter appends dialogue memories to a local JSONL file.
type FileMemoryWriter struct {
	path     string
	mu       sync.Mutex
	types    map[MemoryType]MemoryTypeHandler
	typeList []MemoryType
}

type FileMemoryRecord struct {
	Timestamp     time.Time      `json:"timestamp"`
	SessionID     string         `json:"session_id,omitempty"`
	MemoryType    string         `json:"memory_type,omitempty"`
	MemoryHandler string         `json:"memory_handler,omitempty"`
	Text          string         `json:"text,omitempty"`
	UserText      string         `json:"user_text,omitempty"`
	AssistantText string         `json:"assistant_text,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

func NewFileMemoryWriter(path string) *FileMemoryWriter {
	writer, _ := NewFileMemoryWriterWithHandlers(path, []MemoryTypeHandler{
		&dialogueRawHandler{},
	})
	return writer
}

func NewFileMemoryWriterWithHandlers(path string, handlers []MemoryTypeHandler) (*FileMemoryWriter, error) {
	types, typeList, err := registerMemoryHandlers(handlers)
	if err != nil {
		return nil, err
	}
	return &FileMemoryWriter{
		path:     strings.TrimSpace(path),
		types:    types,
		typeList: typeList,
	}, nil
}

func (w *FileMemoryWriter) WriteDialogue(_ context.Context, sessionID string, userText string, assistantText string) error {
	if w == nil || strings.TrimSpace(w.path) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(w.path), 0o755); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, mt := range w.typeList {
		handler := w.types[mt]
		if handler == nil {
			continue
		}
		text, extra, err := handler.Build(userText, assistantText)
		if err != nil {
			return err
		}
		record := FileMemoryRecord{
			Timestamp:     time.Now().UTC(),
			SessionID:     strings.TrimSpace(sessionID),
			MemoryType:    string(handler.Type()),
			MemoryHandler: string(handler.Type()),
			Text:          text,
			UserText:      strings.TrimSpace(userText),
			AssistantText: strings.TrimSpace(assistantText),
			Metadata:      extra,
		}
		line, err := json.Marshal(record)
		if err != nil {
			return err
		}
		if _, err := f.Write(append(line, '\n')); err != nil {
			return err
		}
	}
	return nil
}
