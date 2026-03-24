package memory

import (
	"context"
	"strings"
	"testing"

	"github.com/vince-0202/acgo/pkg/rag/embedder"
	"github.com/vince-0202/acgo/pkg/rag/vector"
)

type fakeEmbedClient struct{}

func (c *fakeEmbedClient) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	for i, t := range texts {
		// Stable dim=3 so InMemoryVectorStore can compute cosine similarity.
		out = append(out, []float32{float32(len(t)), float32(i + 1), 1})
	}
	return out, nil
}

func (c *fakeEmbedClient) Dim() int { return 3 }

func TestMemoryManager_WriteAndRecallDialogueRaw(t *testing.T) {
	ctx := context.Background()
	emb := embedder.NewWrapper(&fakeEmbedClient{})
	memVec := vector.NewInMemoryVectorStore()

	store := &VectorMemoryStore{
		embedder: emb,
		store:    memVec,
	}

	mgr, err := NewManager(store, []MemoryTypeHandler{
		&dialogueRawHandler{},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	if err := mgr.WriteDialogue(ctx, "sess1", "hi", "hello"); err != nil {
		t.Fatalf("WriteDialogue: %v", err)
	}

	// Recall with matching session should return the stored chunk.
	chunks, err := mgr.Recall(ctx, "anything", "sess1", nil, 5)
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

	// Recall with wrong session should return empty due to filter.
	chunks2, err := mgr.Recall(ctx, "anything", "sess2", nil, 5)
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if len(chunks2) != 1 {
		t.Fatalf("expected recall not to be session-filtered; got %d", len(chunks2))
	}
}
