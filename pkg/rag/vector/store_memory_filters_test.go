package vector

import (
	"context"
	"reflect"
	"testing"
)

func TestInMemoryVectorStore_Search_Filters(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryVectorStore()

	// Two points with identical vectors so score is deterministic; filtering decides the result set.
	if err := store.Upsert(ctx, []Record{
		{
			ID:     "doc-1",
			Vector: []float32{1, 0, 0},
			Text:   "document chunk",
			Metadata: map[string]any{
				"mem_kind": "document",
				"any":      "x",
			},
		},
		{
			ID:     "mem-1",
			Vector: []float32{1, 0, 0},
			Text:   "dialogue memory",
			Metadata: map[string]any{
				"mem_kind":       "memory",
				"memory_type":    "dialogue_raw",
				"session_id":     "s1",
				"memory_handler": "dialogue_raw",
			},
		},
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	// mem_kind filter (string equality)
	res, err := store.Search(ctx, []float32{1, 0, 0}, 5, map[string]any{
		"mem_kind": "memory",
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res) != 1 || res[0].Record.ID != "mem-1" {
		t.Fatalf("mem_kind filter: got %+v", res)
	}

	// memory_type filter ([]string)
	res2, err := store.Search(ctx, []float32{1, 0, 0}, 5, map[string]any{
		"mem_kind":    "memory",
		"memory_type": []string{"dialogue_raw"},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res2) != 1 || res2[0].Record.ID != "mem-1" {
		t.Fatalf("memory_type filter: got %+v", res2)
	}

	// memory_type filter mismatch ([]string) should return empty.
	res3, err := store.Search(ctx, []float32{1, 0, 0}, 5, map[string]any{
		"mem_kind":    "memory",
		"memory_type": []string{"other_type"},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res3) != 0 {
		t.Fatalf("expected empty result for mismatch; got %+v", res3)
	}

	// sanity: metadata preserved
	md := res2[0].Record.Metadata
	if v, ok := md["session_id"].(string); !ok || v != "s1" {
		t.Fatalf("session_id metadata: got %v (%T), want %q", md["session_id"], md["session_id"], "s1")
	}
	if _, ok := md["mem_kind"].(string); !ok {
		t.Fatalf("mem_kind metadata missing, md=%v", md)
	}

	// Ensure filter input types are what our matchFilters expects.
	wantFilterValue := []string{"dialogue_raw"}
	gotFilterValue := map[string]any{"memory_type": []string{"dialogue_raw"}}["memory_type"]
	if !reflect.DeepEqual(wantFilterValue, gotFilterValue) {
		t.Fatalf("test invariant failed")
	}
}
