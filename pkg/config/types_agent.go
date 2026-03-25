package config

import (
	"github.com/vince-0202/acgo/pkg/keys"
	"os"
)

type AgentSetting struct {
	DefaultProvider keys.ProviderType `mapstructure:"default_provider"`
	DefaultBaseUrl  string            `mapstructure:"default_base_url"`
	DefaultApiKey   string            `mapstructure:"default_api_key"`
	DefaultModel    string            `mapstructure:"default_model"`

	DefaultEmbeddingProvider keys.ProviderType `mapstructure:"default_embedding_provider"`
	DefaultEmbeddingModel    string            `mapstructure:"default_embedding_model"` // embedding 模型 ID，如 text-embedding-v3

	Providers []*ProviderSetting `mapstructure:"providers"`
}

func (s *AgentSetting) loadFromEnv() {
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
	if envEmbProvider := os.Getenv("ACGO_DEFAULT_EMBEDDING_PROVIDER"); envEmbProvider != "" {
		s.DefaultEmbeddingProvider = keys.ProviderType(envEmbProvider)
	}
	if envEmbModel := os.Getenv("ACGO_DEFAULT_EMBEDDING_MODEL"); envEmbModel != "" {
		s.DefaultEmbeddingModel = envEmbModel
	}
}

func (s *AgentSetting) LoadAndInit() {
	// 首先取环境变量中的值
	s.loadFromEnv()
	// 设置初始值
	if s.DefaultProvider == "" {
		s.DefaultProvider = keys.ProviderTypeOpenAi
	}
	if s.DefaultBaseUrl == "" {
		s.DefaultBaseUrl = string(keys.GetAPIBaseURL(s.DefaultProvider))
	}

	if len(s.Providers) == 0 {
		defaultProvider := NewDefaultProviderSettingByProvider(s.DefaultProvider)
		defaultProvider.BaseURL = s.DefaultBaseUrl
		defaultProvider.ApiKey = s.DefaultApiKey
		s.Providers = []*ProviderSetting{defaultProvider}
	}
	for _, provider := range s.Providers {
		provider.Init()
	}
	//加载默认model配置
	s.loadDefaultModel()
	// Embedding 默认与 chat 一致，若未配置则用 default_provider 及其默认 embedding 模型
	if s.DefaultEmbeddingProvider == "" {
		s.DefaultEmbeddingProvider = s.DefaultProvider
	}
	if s.DefaultEmbeddingModel == "" && s.DefaultEmbeddingProvider != "" {
		s.DefaultEmbeddingModel = keys.GetDefaultEmbeddingModel(s.DefaultEmbeddingProvider)
		if s.DefaultEmbeddingModel == "" {
			// 该 provider 无专用 embedding 模型，用其第一个 chat 模型
			if p := FindProviderSetting(s.Providers, s.DefaultEmbeddingProvider); p != nil && len(p.Models) > 0 {
				s.DefaultEmbeddingModel = p.Models[0].ID
			}
		}
	}
}

func (s *AgentSetting) loadDefaultModel() {
	if s.DefaultProvider != "" {
		for _, p := range s.Providers {
			if p.Provider == s.DefaultProvider {
				s.DefaultModel = p.Models[0].ID
			}
		}
	}
	if s.DefaultModel == "" {
		s.DefaultModel = s.Providers[0].Models[0].ID
	}
}

// FindProviderSetting returns the first ProviderSetting whose Provider matches.
func FindProviderSetting(providers []*ProviderSetting, typ keys.ProviderType) *ProviderSetting {
	for _, p := range providers {
		if p != nil && p.Provider == typ {
			return p
		}
	}
	return nil
}
