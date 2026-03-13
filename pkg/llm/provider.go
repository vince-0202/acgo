package llm

import (
	"acgo/pkg/keys"
	"context"
)

// Options captures call-level configuration that is independent from a specific provider.
type Options struct {
	Temperature     float32                  // sampling temperature
	MaxOutputTokens int                      // optional override for maximum output tokens
	StopSequences   []string                 // optional stop sequences
	Tools           []Tool                   // tool definitions available to the model
	ToolChoice      string                   // provider-specific tool choice hint, e.g. "auto", "none"
	Metadata        map[string]any           // arbitrary metadata for providers
	ReasoningEffort keys.ReasoningCapability // qualitative reasoning effort
}

// StreamFunc is the unified streaming interface implemented by all providers.
type StreamFunc func(ctx context.Context, model Model, context Context, opts *Options) (<-chan Event, error)

// CompleteFunc runs a non-streaming completion and returns the final assistant message.
type CompleteFunc func(ctx context.Context, model Model, context Context, opts *Options) (Message, Usage, error)

// Provider exposes streaming and non-streaming interfaces as well as its available models.
type Provider interface {
	Name() string
	Stream(ctx context.Context, model Model, context Context, opts *Options) (<-chan Event, error)
	Complete(ctx context.Context, model Model, context Context, opts *Options) (Message, Usage, error)
	Models() []Model
}

// providerRegistry keeps track of all providers.
var providerRegistry = make(map[string]Provider)

// RegisterProvider registers a provider under its Name.
func RegisterProvider(p Provider) {
	if p == nil {
		return
	}
	providerRegistry[p.Name()] = p
	for _, m := range p.Models() {
		RegisterModel(m)
	}
}

// GetProvider retrieves a provider by name.
func GetProvider(name string) (Provider, bool) {
	p, ok := providerRegistry[name]
	return p, ok
}

// ListProviders returns all registered providers.
func ListProviders() []Provider {
	out := make([]Provider, 0, len(providerRegistry))
	for _, p := range providerRegistry {
		out = append(out, p)
	}
	return out
}
