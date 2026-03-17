package rag

import (
	"acgo/pkg/log"
	"acgo/pkg/rag/embedder"
	"acgo/pkg/rag/vector"
	"sync"
)

// defaultRetriever holds the process-wide Retriever instance.
// It is initialized lazily based on the global embedder and vector store.
var (
	muDefaultRetriever sync.RWMutex
	defaultRetriever   *SimpleRetriever
)

// GetRetriever returns the global retriever instance.
// It may return nil if either the embedder or vector store is not available.
func GetRetriever() *SimpleRetriever {
	if defaultRetriever != nil {
		return defaultRetriever
	}
	muDefaultRetriever.Lock()
	defer muDefaultRetriever.Unlock()
	if defaultRetriever != nil {
		return defaultRetriever
	}

	emb := embedder.GetEmbedder()
	if emb == nil {
		log.Debugf("[rag] init retriever: embedder not available")
		return nil
	}
	store := vector.GetVectorStore()
	if store == nil {
		log.Debugf("[rag] init retriever: vector store not available")
		return nil
	}

	defaultRetriever = NewSimpleRetriever(emb, store)
	log.Debugf("[rag] using default retriever with global embedder and vector store")
	return defaultRetriever
}
