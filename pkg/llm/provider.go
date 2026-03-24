package llm

import (
	"acgo/pkg/keys"
)

// Options captures call-level configuration that is independent from a specific provider.
type Options struct {
	Temperature     float32            // sampling temperature
	MaxOutputTokens int                // optional override for maximum output tokens
	StopSequences   []string           // optional stop sequences
	Tools           []Tool             // tool definitions available to the model
	ToolChoice      string             // provider-specific tool choice hint, e.g. "auto", "none"
	Metadata        map[string]any     // arbitrary metadata for providers
	ReasoningEffort keys.ThinkingLevel // qualitative reasoning effort
}

// StreamFunc is the unified streaming interface implemented by all providers.
type StreamFunc func(callCtx Context, model Model, opts *Options) (<-chan Event, error)

// CompleteFunc runs a non-streaming completion and returns the final assistant message.
type CompleteFunc func(callCtx Context, model Model, opts *Options) (Message, Usage, error)

// Provider exposes streaming and non-streaming interfaces as well as its available models.
type Provider interface {
	Name() string
	Stream(callCtx Context, model Model, opts *Options) (<-chan Event, error)
	Complete(callCtx Context, model Model, opts *Options) (Message, Usage, error)
	Models() []Model
}
