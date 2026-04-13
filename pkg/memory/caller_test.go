package memory

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vince-0202/acgo/pkg/rag/embedder"
	"github.com/vince-0202/acgo/pkg/rag/vector"
)

type fakeEmbedClient struct{}

func (c *fakeEmbedClient) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	for i, t := range texts {
		out = append(out, []float32{float32(len(t)), float32(i + 1), 1})
	}
	return out, nil
}

func (c *fakeEmbedClient) Dim() int { return 3 }

func TestVectorStoreMemoryWriterAndCaller(t *testing.T) {
	ctx := context.Background()
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
	if err := writer.WriteDialogue(ctx, "sess1", "hi", "hello"); err != nil {
		t.Fatalf("WriteDialogue: %v", err)
	}

	caller, err := NewVectorStoreMemoryCallerWithStore(store, []MemoryType{DialogueRaw})
	if err != nil {
		t.Fatalf("NewVectorStoreMemoryCallerWithStore: %v", err)
	}
	chunks, err := caller.Recall(ctx, "anything", "sess1", nil, 5)
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Metadata["mem_kind"] != "memory" {
		t.Fatalf("mem_kind mismatch: %v", chunks[0].Metadata["mem_kind"])
	}
	if chunks[0].Metadata["memory_type"] != string(DialogueRaw) {
		t.Fatalf("memory_type mismatch: %v", chunks[0].Metadata["memory_type"])
	}
	if chunks[0].Metadata["session_id"] != "sess1" {
		t.Fatalf("session_id mismatch: %v", chunks[0].Metadata["session_id"])
	}
	txt := chunks[0].Text
	if !strings.Contains(txt, "User: hi") || !strings.Contains(txt, "Assistant: hello") {
		t.Fatalf("chunk text mismatch: %q", txt)
	}
	chunks2, err := caller.Recall(ctx, "anything", "sess2", nil, 5)
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if len(chunks2) != 1 {
		t.Fatalf("expected recall not to be session-filtered; got %d", len(chunks2))
	}
}

func TestFileMemoryCallerRecall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.jsonl")
	writer, err := NewFileMemoryWriterWithHandlers(path, []MemoryTypeHandler{
		&dialogueRawHandler{},
	})
	if err != nil {
		t.Fatalf("NewFileMemoryWriterWithHandlers: %v", err)
	}
	if err := writer.WriteDialogue(context.Background(), "sess1", "likes go", "using acgo"); err != nil {
		t.Fatalf("WriteDialogue: %v", err)
	}
	if err := writer.WriteDialogue(context.Background(), "sess2", "likes rust", "using cargo"); err != nil {
		t.Fatalf("WriteDialogue: %v", err)
	}

	caller := NewFileMemoryCaller(path)
	chunks, err := caller.Recall(context.Background(), "likes go", "", nil, 2)
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatalf("expected file recall results")
	}
	if !strings.Contains(chunks[0].Text, "likes go") {
		t.Fatalf("unexpected top chunk: %+v", chunks[0])
	}
	if chunks[0].Metadata["memory_type"] != string(DialogueRaw) {
		t.Fatalf("memory_type mismatch: %v", chunks[0].Metadata["memory_type"])
	}
}

func TestFileMemoryCallerReadsLegacyFileRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	record := FileMemoryRecord{
		SessionID:     "sess1",
		UserText:      "legacy user",
		AssistantText: "legacy assistant",
	}
	line, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	w := bufio.NewWriter(f)
	if _, err := w.Write(append(line, '\n')); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	caller := NewFileMemoryCaller(path)
	chunks, err := caller.Recall(context.Background(), "legacy user", "", nil, 1)
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if !strings.Contains(chunks[0].Text, "legacy assistant") {
		t.Fatalf("unexpected chunk text: %q", chunks[0].Text)
	}
}
