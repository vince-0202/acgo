package rag

import (
	"context"
	"time"

	"acgo/pkg/log"
)

// SimpleRetriever is a basic implementation of Retriever.
// It delegates to an Embedder and VectorStore and converts hits into DocumentChunk values.
type SimpleRetriever struct {
	Embedder    Embedder
	VectorStore VectorStore
}

func NewSimpleRetriever(embedder Embedder, store VectorStore) *SimpleRetriever {
	return &SimpleRetriever{
		Embedder:    embedder,
		VectorStore: store,
	}
}

func (r *SimpleRetriever) Retrieve(ctx context.Context, query string, topK int, filters map[string]any) ([]DocumentChunk, error) {
	if r == nil || r.Embedder == nil || r.VectorStore == nil {
		return nil, nil
	}
	start := time.Now()
	log.Debugf("[rag] Retriever.Retrieve: query_len=%d topK=%d", len(query), topK)
	vec, err := r.Embedder.EmbedQuery(ctx, query)
	if err != nil {
		log.Debugf("[rag] Retriever.Retrieve: EmbedQuery err=%v", err)
		return nil, err
	}
	log.Debugf("[rag] Retriever.Retrieve: embed done dim=%d elapsed=%v", len(vec), time.Since(start))
	searchStart := time.Now()
	results, err := r.VectorStore.Search(ctx, vec, topK, filters)
	if err != nil {
		log.Debugf("[rag] Retriever.Retrieve: Search err=%v", err)
		return nil, err
	}
	log.Debugf("[rag] Retriever.Retrieve: search done hits=%d elapsed=%v", len(results), time.Since(searchStart))
	chunks := make([]DocumentChunk, 0, len(results))
	for _, res := range results {
		chunks = append(chunks, DocumentChunk{
			ID:       res.Record.ID,
			Text:     res.Record.Text,
			Score:    res.Score,
			Metadata: res.Record.Metadata,
		})
	}
	log.Debugf("[rag] Retriever.Retrieve: done chunks=%d total_elapsed=%v", len(chunks), time.Since(start))
	return chunks, nil
}
