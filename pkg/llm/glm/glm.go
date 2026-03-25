package glm

import (
	"github.com/vince-0202/acgo/pkg/config"
	"github.com/vince-0202/acgo/pkg/llm"
	"github.com/vince-0202/acgo/pkg/llm/openai"
)

// Client wraps OpenAI-compatible client for Zhipu GLM.
type Client openai.Client

// NewClient creates a GLM provider using the OpenAI-compatible Chat Completions API.
func NewClient(setting *config.ProviderSetting) llm.Provider {
	return openai.NewClient(setting)
}
