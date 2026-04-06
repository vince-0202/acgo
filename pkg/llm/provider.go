package llm

import (
	"context"
	"github.com/vince-0202/acgo/pkg/communi"
	"github.com/vince-0202/acgo/pkg/keys"
)

// Options captures call-level configuration that is independent from a specific provider.
type Options struct {
	Temperature     float32              // sampling temperature
	MaxOutputTokens int                  // optional override for maximum output tokens
	StopSequences   []string             // optional stop sequences
	Tools           []communi.ToolSchema // tool definitions available to the model
	ToolChoice      string               // provider-specific tool choice hint, e.g. "auto", "none"
	Metadata        map[string]any       // arbitrary metadata for providers
	ReasoningEffort keys.ThinkingLevel   // qualitative reasoning effort
}

// Provider exposes streaming and non-streaming interfaces as well as its available models.
type Provider interface {
	Name() string
	Stream(ctx context.Context, model Model, message []communi.Message, opts *Options) (<-chan communi.LLMEvent, error)
	Complete(ctx context.Context, model Model, message []communi.Message, opts *Options) (communi.Message, Usage, error)
	Models() []Model
}
