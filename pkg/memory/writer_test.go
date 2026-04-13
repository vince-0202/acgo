package memory

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/vince-0202/acgo/pkg/rag/embedder"
	"github.com/vince-0202/acgo/pkg/rag/vector"
)

func TestFileMemoryWriterAppendsJSONL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory", "mem.jsonl")
	writer := NewFileMemoryWriter(path)

	if err := writer.WriteDialogue(context.Background(), "sess-1", "u", "a"); err != nil {
		t.Fatalf("WriteDialogue error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}

	var record FileMemoryRecord
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if record.SessionID != "sess-1" || record.UserText != "u" || record.AssistantText != "a" {
		t.Fatalf("unexpected record: %+v", record)
	}
}

func TestVectorStoreMemoryWriterWritesDialogue(t *testing.T) {
	emb := embedder.NewWrapper(&fakeEmbedClient{})
	memVec := vector.NewInMemoryVectorStore()
	store := &VectorMemoryStore{
		embedder: emb,
		store:    memVec,
	}
	writer, err := NewVectorStoreMemoryWriterWithStore(store, []MemoryTypeHandler{
		&dialogueRawHandler{},
	})
	if err != nil {
		t.Fatalf("NewVectorStoreMemoryWriterWithStore: %v", err)
	}

	if err := writer.WriteDialogue(context.Background(), "sess-1", "u", "a"); err != nil {
		t.Fatalf("WriteDialogue error: %v", err)
	}

	caller, err := NewVectorStoreMemoryCallerWithStore(store, []MemoryType{DialogueRaw})
	if err != nil {
		t.Fatalf("NewVectorStoreMemoryCallerWithStore: %v", err)
	}
	chunks, err := caller.Recall(context.Background(), "u", "", nil, 5)
	if err != nil {
		t.Fatalf("Recall error: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
}
