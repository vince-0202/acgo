package llm

// InputCapability describes what kind of input a model supports.
// It mirrors the capabilities exposed by pi-ai (e.g. text, image).
type InputCapability string

const (
	InputCapabilityText  InputCapability = "text"
	InputCapabilityImage InputCapability = "image"
)

// ReasoningCapability describes the qualitative reasoning strength of a model.
// It is intentionally coarse-grained to stay provider-agnostic.
type ReasoningCapability string

const (
	ReasoningNone    ReasoningCapability = "none"
	ReasoningMinimal ReasoningCapability = "minimal"
	ReasoningLow     ReasoningCapability = "low"
	ReasoningMedium  ReasoningCapability = "medium"
	ReasoningHigh    ReasoningCapability = "high"
	ReasoningXHigh   ReasoningCapability = "xhigh"
)

// Model describes an LLM model in a provider-neutral way, similar to pi-ai's Model type.
type Model struct {
	ID            string              // unique identifier within provider
	Name          string              // human readable name
	API           string              // underlying API family (e.g. openai-chat, openai-responses)
	Provider      string              // provider identifier (openai, anthropic, etc.)
	ContextWindow int                 // approximate context window in tokens
	MaxTokens     int                 // maximum generation tokens
	Input         []InputCapability   // supported input modalities
	Reasoning     ReasoningCapability // reasoning capability level
}

// Registry keeps track of all known models.
type Registry struct {
	models map[string]Model
}

// globalRegistry is used by helper functions for simple scenarios.
var globalRegistry = NewRegistry()

// NewRegistry creates an empty model registry.
func NewRegistry() *Registry {
	return &Registry{
		models: make(map[string]Model),
	}
}

// Register adds or replaces a model in the registry.
func (r *Registry) Register(m Model) {
	if r == nil {
		return
	}
	r.models[m.ID] = m
}

// Get retrieves a model by ID.
func (r *Registry) Get(id string) (Model, bool) {
	if r == nil {
		return Model{}, false
	}
	m, ok := r.models[id]
	return m, ok
}

// List returns all registered models.
func (r *Registry) List() []Model {
	if r == nil {
		return nil
	}
	out := make([]Model, 0, len(r.models))
	for _, m := range r.models {
		out = append(out, m)
	}
	return out
}

// RegisterModel registers a model in the global registry.
func RegisterModel(m Model) {
	globalRegistry.Register(m)
}

// GetModel retrieves a model from the global registry.
func GetModel(id string) (Model, bool) {
	return globalRegistry.Get(id)
}

// ListModels returns all models from the global registry.
func ListModels() []Model {
	return globalRegistry.List()
}

