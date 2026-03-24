package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/vince-0202/acgo/pkg/agent"
	"github.com/vince-0202/acgo/pkg/memory"
)

type dialogueRawTestHandler struct{}

func (h *dialogueRawTestHandler) Type() memory.MemoryType { return memory.DialogueRaw }
func (h *dialogueRawTestHandler) Build(userText string, assistantText string) (string, map[string]any, error) {
	return "User: " + userText + "\nAssistant: " + assistantText, nil, nil
}

type fakeMemoryStore struct {
	lastQuery   string
	lastTopK    int
	lastFilters map[string]any
}

func (s *fakeMemoryStore) Upsert(_ context.Context, _ string, _ map[string]any) (string, error) {
	return "noop", nil
}

func (s *fakeMemoryStore) Search(_ context.Context, query string, topK int, filters map[string]any) ([]memory.Chunk, error) {
	s.lastQuery = query
	s.lastTopK = topK
	s.lastFilters = filters
	return []memory.Chunk{
		{
			ID:    "c1",
			Text:  "User: hi\nAssistant: hello",
			Score: 0.9,
			Metadata: map[string]any{
				"mem_kind":    "memory",
				"memory_type": string(memory.DialogueRaw),
				"session_id":  "sess-1",
			},
		},
	}, nil
}

func TestMemoryRecallTool_ParseAndFormat(t *testing.T) {
	store := &fakeMemoryStore{}
	mgr, err := memory.NewManager(store, []memory.MemoryTypeHandler{
		&dialogueRawTestHandler{},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	tool := &memoryRecallTool{manager: mgr}

	ctx := memory.WithSessionID(context.Background(), "sess-1")
	args := json.RawMessage(`{"query":"q1","memory_types":["dialogue_raw"],"top_k":3}`)
	res, err := tool.Execute(ctx, "call-1", args, nil)
	if err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if res.IsError {
		t.Fatalf("Execute returned IsError: %v (%s)", res.IsError, res.Content)
	}
	if !strings.Contains(res.Content, "type=dialogue_raw") {
		t.Fatalf("output missing type=dialogue_raw: %q", res.Content)
	}
	if !strings.Contains(res.Content, "Assistant: hello") {
		t.Fatalf("output missing assistant content: %q", res.Content)
	}

	if store.lastTopK != 3 {
		t.Fatalf("topK=%d, want 3", store.lastTopK)
	}
	// Verify filters: memory_recall must include mem_kind + memory_type + session_id.
	if store.lastFilters == nil {
		t.Fatalf("expected filters to be passed to store")
	}
	if store.lastFilters["mem_kind"] != "memory" {
		t.Fatalf("mem_kind filter mismatch: %v", store.lastFilters["mem_kind"])
	}
	if mt, ok := store.lastFilters["memory_type"].([]string); !ok || len(mt) != 1 || mt[0] != "dialogue_raw" {
		t.Fatalf("memory_type filter mismatch: %#v", store.lastFilters["memory_type"])
	}
	if _, ok := store.lastFilters["session_id"]; ok {
		t.Fatalf("expected no session_id filter, got %v", store.lastFilters["session_id"])
	}

	_ = agent.ToolResult{} // silence unused import paranoia if gofmt changes order
}
