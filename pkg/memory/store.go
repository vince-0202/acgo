package memory

import (
	"context"
	"fmt"
	"github.com/vince-0202/acgo/pkg/keys"
	"github.com/vince-0202/acgo/pkg/rag/embedder"
	"github.com/vince-0202/acgo/pkg/rag/vector"
	"github.com/vince-0202/acgo/pkg/utils"
)

// Chunk is a recalled memory chunk.
type Chunk struct {
	ID       string
	Text     string
	Score    float32
	Metadata map[string]any
}

// Store abstracts memory storage backends.
type Store interface {
	Upsert(ctx context.Context, chunkText string, metadata map[string]any) (chunkID string, err error)
	Search(ctx context.Context, query string, topK int, filters map[string]any) ([]Chunk, error)
}

// VectorMemoryStore stores memory as vectors in the existing rag vector store.
type VectorMemoryStore struct {
	embedder *embedder.Wrapper
	store    vector.Store
}

func NewVectorMemoryStore() (*VectorMemoryStore, error) {
	emb := embedder.GetEmbedder()
	if emb == nil {
		return nil, fmt.Errorf("embedding provider not configured")
	}
	st := vector.GetVectorStore()
	if st == nil {
		return nil, fmt.Errorf("vector store not configured")
	}
	return &VectorMemoryStore{
		embedder: emb,
		store:    st,
	}, nil
}

func (s *VectorMemoryStore) Upsert(ctx context.Context, chunkText string, metadata map[string]any) (string, error) {
	if s == nil || s.embedder == nil || s.store == nil {
		return "", fmt.Errorf("memory store not initialized")
	}
	if chunkText == "" {
		return "", nil
	}
	vec, err := s.embedder.EmbedQuery(ctx, chunkText)
	if err != nil {
		return "", err
	}
	// Qdrant point IDs in this project are treated as UUIDs (see rag ingest which uses UUID v4).
	// If we use non-UUID IDs (e.g. snowflake numeric strings), qdrant.NewID(...) fails with:
	// "Unable to parse UUID".
	chunkID := utils.NextID(keys.IdKindUUID)
	if metadata == nil {
		metadata = map[string]any{}
	}
	rec := vector.Record{
		ID:       chunkID,
		Vector:   vec,
		Text:     chunkText,
		Metadata: metadata,
	}
	if err := s.store.Upsert(ctx, []vector.Record{rec}); err != nil {
		return "", err
	}
	return chunkID, nil
}

func (s *VectorMemoryStore) Search(ctx context.Context, query string, topK int, filters map[string]any) ([]Chunk, error) {
	if s == nil || s.embedder == nil || s.store == nil {
		return nil, fmt.Errorf("memory store not initialized")
	}
	vec, err := s.embedder.EmbedQuery(ctx, query)
	if err != nil {
		return nil, err
	}
	results, err := s.store.Search(ctx, vec, topK, filters)
	if err != nil {
		return nil, err
	}
	out := make([]Chunk, 0, len(results))
	for _, r := range results {
		out = append(out, Chunk{
			ID:       r.Record.ID,
			Text:     r.Record.Text,
			Score:    r.Score,
			Metadata: r.Record.Metadata,
		})
	}
	return out, nil
}
