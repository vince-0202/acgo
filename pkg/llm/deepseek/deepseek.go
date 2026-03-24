package deepseek

import (
	"github.com/vince-0202/acgo/pkg/config"
	"github.com/vince-0202/acgo/pkg/llm"
	"github.com/vince-0202/acgo/pkg/llm/openai"
)

type Client openai.Client

// NewClient creates a new OpenAI-compatible client. baseURL is the API root (e.g. https://api.openai.com/v1).
// If baseURL is empty, it defaults to OpenAI. Options can override provider name, API key env, and models.
func NewClient(setting *config.ProviderSetting) llm.Provider {
	return openai.NewClient(setting)
}
