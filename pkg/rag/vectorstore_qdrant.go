package rag

import (
	"context"
	"errors"
	"fmt"
)

// QdrantClient is the minimal subset of a Qdrant client that VectorStoreQdrant needs.
// A concrete implementation can live in another package and satisfy this interface.
type QdrantClient interface {
	UpsertPoints(ctx context.Context, collection string, points []QdrantPoint) error
	Search(ctx context.Context, collection string, vector []float32, topK int, filters map[string]any) ([]QdrantPoint, error)
	DeleteByIDs(ctx context.Context, collection string, ids []string) error
}

// QdrantPoint is a simplified point representation for Qdrant.
type QdrantPoint struct {
	ID      string
	Vector  []float32
	Payload map[string]any
	Score   float32
}

// VectorStoreQdrant adapts a QdrantClient to the VectorStore interface.
type VectorStoreQdrant struct {
	client     QdrantClient
	collection string
}

func NewVectorStoreQdrant(client QdrantClient, collection string) (*VectorStoreQdrant, error) {
	if client == nil {
		return nil, errors.New("qdrant client is nil")
	}
	if collection == "" {
		return nil, errors.New("qdrant collection name is empty")
	}
	return &VectorStoreQdrant{
		client:     client,
		collection: collection,
	}, nil
}

func (s *VectorStoreQdrant) Upsert(ctx context.Context, records []VectorRecord) error {
	if len(records) == 0 {
		return nil
	}
	points := make([]QdrantPoint, 0, len(records))
	for _, r := range records {
		// Copy metadata so we can safely enrich it with text.
		md := make(map[string]any, len(r.Metadata)+1)
		for k, v := range r.Metadata {
			md[k] = v
		}
		// Persist original chunk text in payload so retriever/tool can display it.
		if r.Text != "" {
			md["text"] = r.Text
		}
		points = append(points, QdrantPoint{
			ID:      r.ID,
			Vector:  r.Vector,
			Payload: md,
		})
	}
	return s.client.UpsertPoints(ctx, s.collection, points)
}

func (s *VectorStoreQdrant) Search(ctx context.Context, query []float32, topK int, filters map[string]any) ([]SearchResult, error) {
	if len(query) == 0 {
		return nil, fmt.Errorf("qdrant search: empty query vector")
	}
	if topK <= 0 {
		topK = 5
	}
	points, err := s.client.Search(ctx, s.collection, query, topK, filters)
	if err != nil {
		return nil, err
	}
	results := make([]SearchResult, 0, len(points))
	for _, p := range points {
		text, _ := p.Payload["text"].(string)
		results = append(results, SearchResult{
			Record: VectorRecord{
				ID:       p.ID,
				Vector:   p.Vector,
				Text:     text,
				Metadata: p.Payload,
			},
			Score: p.Score,
		})
	}
	return results, nil
}

func (s *VectorStoreQdrant) DeleteByDocID(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	return s.client.DeleteByIDs(ctx, s.collection, ids)
}
