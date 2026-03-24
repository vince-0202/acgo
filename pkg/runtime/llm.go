package runtime

import (
	"github.com/vince-0202/acgo/pkg/llm"
)

// providerRegistry keeps track of all providers.
var providerRegistry = make(map[string]llm.Provider)

// RegisterProvider registers a provider under its Name.
func RegisterProvider(p llm.Provider) {
	if p == nil {
		return
	}
	providerRegistry[p.Name()] = p
	for _, m := range p.Models() {
		RegisterModel(m)
	}
}

// GetProvider retrieves a provider by name.
func GetProvider(name string) (llm.Provider, bool) {
	p, ok := providerRegistry[name]
	return p, ok
}

// ListProviders returns all registered providers.
func ListProviders() []llm.Provider {
	out := make([]llm.Provider, 0, len(providerRegistry))
	for _, p := range providerRegistry {
		out = append(out, p)
	}
	return out
}

// Registry keeps track of all known models.
type Registry struct {
	models map[string]llm.Model
}

// globalRegistry is used by helper functions for simple scenarios.
var globalRegistry = NewRegistry()

// NewRegistry creates an empty model registry.
func NewRegistry() *Registry {
	return &Registry{
		models: make(map[string]llm.Model),
	}
}

// Register adds or replaces a model in the registry.
func (r *Registry) Register(m llm.Model) {
	if r == nil {
		return
	}
	r.models[m.ID] = m
}

// Get retrieves a model by ID.
func (r *Registry) Get(id string) (llm.Model, bool) {
	if r == nil {
		return llm.Model{}, false
	}
	m, ok := r.models[id]
	return m, ok
}

// List returns all registered models.
func (r *Registry) List() []llm.Model {
	if r == nil {
		return nil
	}
	out := make([]llm.Model, 0, len(r.models))
	for _, m := range r.models {
		out = append(out, m)
	}
	return out
}

// RegisterModel registers a model in the global registry.
func RegisterModel(m llm.Model) {
	globalRegistry.Register(m)
}

// GetModel retrieves a model from the global registry.
func GetModel(id string) (llm.Model, bool) {
	return globalRegistry.Get(id)
}

// ListModels returns all models from the global registry.
func ListModels() []llm.Model {
	return globalRegistry.List()
}
