package embedder

import (
	"github.com/vince-0202/acgo/pkg/config"
	"github.com/vince-0202/acgo/pkg/keys"
	"github.com/vince-0202/acgo/pkg/llm/openai"
	"github.com/vince-0202/acgo/pkg/log"
	"sync"
)

// defaultEmbedder holds the process-wide Embedder instance.
// It is intended to be initialized once (lazily) and then reused.
var (
	muDefaultEmbedder sync.RWMutex
	defaultEmbedder   *Wrapper
)

// GetEmbedder returns the global embedder instance.
// It may return nil if no embedding provider could be configured.
func GetEmbedder() *Wrapper {
	if defaultEmbedder != nil {
		return defaultEmbedder
	}
	muDefaultEmbedder.Lock()
	defer muDefaultEmbedder.Unlock()
	if defaultEmbedder != nil {
		return defaultEmbedder
	}

	settings, err := config.LoadSettings()
	if err != nil {
		log.Debugf("[rag] init embedder: load settings err=%v", err)
		return nil
	}
	if len(settings.Agent.Providers) == 0 || settings.Agent.Providers[0] == nil {
		log.Debugf("[rag] init embedder: no providers configured")
		return nil
	}

	// Resolve embedding provider.
	embProvider := settings.Agent.DefaultEmbeddingProvider
	if embProvider == "" {
		embProvider = settings.Agent.DefaultProvider
	}
	provider := config.FindProviderSetting(settings.Agent.Providers, embProvider)
	if provider == nil {
		provider = settings.Agent.Providers[0]
	}

	// Resolve embedding model.
	embedModel := settings.Agent.DefaultEmbeddingModel
	if embedModel == "" {
		embedModel = keys.GetDefaultEmbeddingModel(provider.Provider)
		if embedModel == "" {
			embedModel = settings.Agent.DefaultModel
		}
	}

	embClient := openai.NewEmbeddingClient(provider.BaseURL, provider.ApiKey, embedModel, nil)
	defaultEmbedder = NewWrapper(embClient)
	log.Debugf("[rag] using embedder provider=%s model=%s", provider.Provider, embedModel)
	return defaultEmbedder
}
