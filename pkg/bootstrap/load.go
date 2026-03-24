package bootstrap

import (
	"acgo/pkg/config"
	"acgo/pkg/keys"
	"acgo/pkg/llm"
	"acgo/pkg/llm/deepseek"
	"acgo/pkg/llm/openai"
	"acgo/pkg/llm/qwen"
	"acgo/pkg/runtime"
)

// Load setting and runtime
func Load(options ...config.SettingOption) (*config.Settings, error) {
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
		default:
			continue
		}
	}
	return providers
}
