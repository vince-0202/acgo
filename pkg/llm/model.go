package llm

import (
	"github.com/vince-0202/acgo/pkg/config"
)

// Model describes an LLM model in a provider-neutral way, similar to pi-ai's Model type.
type Model struct {
	config.ModelSetting
	Provider string // provider identifier (openai, anthropic, etc.)
}
