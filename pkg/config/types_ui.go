package config

import (
	"acgo/pkg/keys"
	"os"
)

type Tui struct {
	Agent TuiAgent `mapstructure:"agent"`
}

func (t *Tui) LoadAndInit() {
	t.Agent.LoadAndInit()
}

type TuiAgent struct {
	DefaultProvider keys.ProviderType  `mapstructure:"default_provider"`
	DefaultBaseUrl  string             `mapstructure:"default_base_url"`
	DefaultApiKey   string             `mapstructure:"default_api_key"`
	DefaultModel    string             `mapstructure:"default_model"`
	Providers       []*ProviderSetting `mapstructure:"providers"`
}

func (s *TuiAgent) loadFromEnv() {
	if envDefaultProvider := os.Getenv("ACGO_DEFAULT_PROVIDER"); envDefaultProvider != "" {
		s.DefaultProvider = keys.ProviderType(envDefaultProvider)
	}
	if envDefaultBaseUrl := os.Getenv("ACGO_DEFAULT_BASE_URL"); envDefaultBaseUrl != "" {
		s.DefaultBaseUrl = envDefaultBaseUrl
	}
	if envDefaultApiKey := os.Getenv("ACGO_DEFAULT_API_KEY"); envDefaultApiKey != "" {
		s.DefaultApiKey = envDefaultApiKey
	}
	if envDefaultModel := os.Getenv("ACGO_DEFAULT_MODEL"); envDefaultModel != "" {
		s.DefaultModel = envDefaultModel
	}
}

func (s *TuiAgent) LoadAndInit() {
	// 首先取环境变量中的值
	s.loadFromEnv()
	// 设置初始值
	if s.DefaultProvider == "" {
		s.DefaultProvider = keys.ProviderTypeOpenAi
	}
	if s.DefaultBaseUrl == "" {
		s.DefaultBaseUrl = string(keys.GetAPIBaseURL(s.DefaultProvider))
	}

	if s.Providers == nil || len(s.Providers) == 0 {
		defaultProvider := NewDefaultProviderSettingByProvider(s.DefaultProvider)
		defaultProvider.BaseURL = s.DefaultBaseUrl
		defaultProvider.ApiKey = s.DefaultApiKey
		s.Providers = []*ProviderSetting{defaultProvider}
	}

	if s.DefaultModel == "" {
		s.DefaultModel = s.Providers[0].Models[0].ID
	}
}
