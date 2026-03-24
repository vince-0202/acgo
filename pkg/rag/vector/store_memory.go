package vector

import (
	"context"
	"errors"
	"math"
	"sync"

	"github.com/vince-0202/acgo/pkg/log"
)

// InMemoryVectorStore is a simple vector store implementation backed by a map.
// It is intended for testing and local experimentation, not for production use.
type InMemoryVectorStore struct {
	mu    sync.RWMutex
	items map[string]Record
}

func NewInMemoryVectorStore() *InMemoryVectorStore {
	return &InMemoryVectorStore{
		items: make(map[string]Record),
	}
}

func (s *InMemoryVectorStore) Upsert(_ context.Context, records []Record) error {
	if len(records) == 0 {
		return nil
	}
	log.Debugf("[rag] InMemoryVectorStore.Upsert: records=%d", len(records))
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range records {
		if r.ID == "" {
			continue
		}
		s.items[r.ID] = r
	}
	log.Debugf("[rag] InMemoryVectorStore.Upsert: total_items=%d", len(s.items))
	return nil
}

func (s *InMemoryVectorStore) Search(_ context.Context, query []float32, topK int, filters map[string]any) ([]SearchResult, error) {
	if len(query) == 0 {
		return nil, errors.New("query vector is empty")
	}
	if topK <= 0 {
		topK = 5
	}
	s.mu.RLock()
	storeSize := len(s.items)
	results := make([]SearchResult, 0, storeSize)
	for _, r := range s.items {
		// For in-memory we implement a small subset of metadata filters so
		// memory recall can be exercised in local tests.
		// (If you pass unsupported filter types we just skip filtering and
		// return a broader result set.)
		if len(filters) > 0 && !matchFilters(r, filters) {
			continue
		}
		if len(r.Vector) != len(query) {
			continue
		}
		score := cosineSimilarity(r.Vector, query)
		results = append(results, SearchResult{Record: r, Score: score})
	}
	s.mu.RUnlock()
	log.Debugf("[rag] InMemoryVectorStore.Search: query_dim=%d topK=%d store_size=%d candidates=%d", len(query), topK, storeSize, len(results))
	// partial sort for topK
	if len(results) <= topK {
		sortByScoreDesc(results)
		log.Debugf("[rag] InMemoryVectorStore.Search: returned=%d", len(results))
		return results, nil
	}
	sortByScoreDesc(results)
	out := results[:topK]
	log.Debugf("[rag] InMemoryVectorStore.Search: returned=%d", len(out))
	return out, nil
}

func matchFilters(r Record, filters map[string]any) bool {
	for key, rawVal := range filters {
		if key == "" || rawVal == nil {
			continue
		}
		mdVal, ok := r.Metadata[key]
		if !ok {
			return false
		}

		switch v := rawVal.(type) {
		case string:
			s, ok := mdVal.(string)
			if !ok || s != v {
				return false
			}
		case []string:
			s, ok := mdVal.(string)
			if !ok {
				return false
			}
			if !containsString(v, s) {
				return false
			}
		case []any:
			var ss []string
			for _, it := range v {
				s, ok := it.(string)
				if ok && s != "" {
					ss = append(ss, s)
				}
			}
			s, ok := mdVal.(string)
			if !ok || !containsString(ss, s) {
				return false
			}
		default:
			// Unsupported filter type: fail closed to avoid returning wrong memory sets.
			return false
		}
	}
	return true
}

func containsString(ss []string, s string) bool {
	for _, it := range ss {
		if it == s {
			return true
		}
	}
	return false
}

func (s *InMemoryVectorStore) DeleteByDocID(_ context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	log.Debugf("[rag] InMemoryVectorStore.DeleteByDocID: ids=%d", len(ids))
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range ids {
		delete(s.items, id)
	}
	log.Debugf("[rag] InMemoryVectorStore.DeleteByDocID: remaining_items=%d", len(s.items))
	return nil
}

func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot float32
	var na, nb float32
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / float32(math.Sqrt(float64(na*nb)))
}

func sortByScoreDesc(items []SearchResult) {
	// simple insertion sort is fine for small slices and avoids pulling in sort package unnecessarily
	for i := 1; i < len(items); i++ {
		j := i
		for j > 0 && items[j-1].Score < items[j].Score {
			items[j-1], items[j] = items[j], items[j-1]
			j--
		}
	}
}
