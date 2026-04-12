package setting

import (
	"github.com/vince-0202/acgo/pkg/config"
	"github.com/vince-0202/acgo/pkg/keys"
	"github.com/vince-0202/acgo/pkg/llm"
	"github.com/vince-0202/acgo/pkg/llm/deepseek"
	"github.com/vince-0202/acgo/pkg/llm/glm"
	"github.com/vince-0202/acgo/pkg/llm/openai"
	"github.com/vince-0202/acgo/pkg/llm/qwen"
	"github.com/vince-0202/acgo/pkg/runtime"
)

// LoadAndRuntimeInit loads settings and registers providers into runtime.
func LoadAndRuntimeInit(options ...config.SettingOption) (*config.Settings, error) {
	settingConfig := config.NewSettingConfig(options...)
	settings, err := config.LoadSettingsByConfig(settingConfig)
	if err != nil {
		return nil, err
	}
	for _, provider := range newProviderBySettings(settings.Agent) {
		runtime.RegisterProvider(provider)
	}
	return settings, nil
}

func newProviderBySettings(settings config.AgentSetting) []llm.Provider {
	var providers []llm.Provider
	for _, providerSetting := range settings.Providers {
		switch providerSetting.Provider {
		case keys.ProviderTypeOpenAi:
			providers = append(providers, openai.NewClient(providerSetting))
		case keys.ProviderTypeDeepSeek:
			providers = append(providers, deepseek.NewClient(providerSetting))
		case keys.ProviderTypeQwen:
			providers = append(providers, qwen.NewClient(providerSetting))
		case keys.ProviderTypeGLM:
			providers = append(providers, glm.NewClient(providerSetting))
		default:
			continue
		}
	}
	return providers
}
