package vector

import (
	"github.com/vince-0202/acgo/pkg/config"
	"github.com/vince-0202/acgo/pkg/keys"
	"github.com/vince-0202/acgo/pkg/log"
	"sync"
)

// defaultVectorStore holds the process-wide VectorStore instance.
// It is intended to be initialized once at startup (e.g. from CLI main)
// and then reused by all RAG components.
var (
	muDefaultVectorStore sync.RWMutex
	defaultVectorStore   Store
)

// GetVectorStore returns the global VectorStore previously set via SetVectorStore.
// It may return nil if no store was configured.
func GetVectorStore() Store {
	if defaultVectorStore != nil {
		return defaultVectorStore
	}
	muDefaultVectorStore.Lock()
	defer muDefaultVectorStore.Unlock()
	// Single log config for the whole project: load config and init log once before any command.
	settings, err := config.LoadSettings()
	if err != nil {
		defaultVectorStore = NewInMemoryVectorStore()
		return defaultVectorStore
	}
	// Initialize global RAG VectorStore singleton based on configuration.
	switch settings.Rag.VectorStoreType {
	case keys.VectorStoreTypeQdrant:
		s, err := NewVectorStoreQdrantFromConfig(settings.Rag.Qdrant)
		if err != nil {
			log.Debugf("[root] init qdrant vector store err=%v", err)
		} else {
			defaultVectorStore = s
			log.Debugf("[root] using Qdrant vector store host=%s port=%d collection=%s",
				settings.Rag.Qdrant.Host, settings.Rag.Qdrant.Port, settings.Rag.Qdrant.Collection)
		}
	default:
		defaultVectorStore = NewInMemoryVectorStore()
		log.Debugf("[root] using in-memory vector store")
	}

	return defaultVectorStore
}
