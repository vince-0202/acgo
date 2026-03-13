package config

import (
	"acgo/pkg/keys"
	"os"
	"path/filepath"
	"strings"
)

// Settings represents the merged configuration for acgo.
type Settings struct {
	DefaultProvider string            `mapstructure:"default_provider"`
	DefaultModelID  string            `mapstructure:"default_model_id"`
	APIKeys         map[string]string `mapstructure:"api_keys"`
	Log             Log               `mapstructure:"log"`
	Tui             Tui               `mapstructure:"tui"`
}

type ProviderSetting struct {
	Provider keys.ProviderType `mapstructure:"provider"` // e.g. "openai"
	BaseURL  string            `mapstructure:"base_url"`
	ApiKey   string            `mapstructure:"api_key"` // env var name for API key, e.g. "OPENAI_API_KEY", "DEEPSEEK_API_KEY"
	Models   []ModelSetting    `mapstructure:"Models"`
}

func NewDefaultProviderSettingByProvider(providerType keys.ProviderType) *ProviderSetting {
	switch providerType {
	case keys.ProviderTypeOpenAi:
		return newDefaultOpenaiProviderSetting()
	case keys.ProviderTypeDeepSeek:
		return newDefaultDeepseekProviderSetting()
	default:
		panic("invalid Provider type")
	}
}

func newDefaultOpenaiProviderSetting() *ProviderSetting {
	var openai keys.ProviderType = keys.ProviderTypeOpenAi
	return &ProviderSetting{
		Provider: openai,
		BaseURL:  string(keys.GetAPIBaseURL(openai)),
		ApiKey:   "API_KEY",
		Models:   NewDefaultModelsByProvider(openai),
	}
}

func newDefaultDeepseekProviderSetting() *ProviderSetting {
	var deepseek keys.ProviderType = keys.ProviderTypeDeepSeek
	return &ProviderSetting{
		Provider: deepseek,
		BaseURL:  string(keys.GetAPIBaseURL(deepseek)),
		ApiKey:   "API_KEY",
		Models:   NewDefaultModelsByProvider(deepseek),
	}
}

type ModelSetting struct {
	ID            string                   `mapstructure:"id"`             // unique identifier within Provider
	Name          string                   `mapstructure:"name"`           // human readable name
	API           string                   `mapstructure:"api"`            // underlying API family (e.g. openai-chat, openai-responses)
	ContextWindow int                      `mapstructure:"context_window"` // approximate context window in tokens
	MaxTokens     int                      `mapstructure:"max_tokens"`     // maximum generation tokens
	Input         []keys.InputCapability   `mapstructure:"input"`          // supported input modalities
	Reasoning     keys.ReasoningCapability `mapstructure:"reasoning"`      // reasoning capability level
}

func NewDefaultModelsByProvider(providerType keys.ProviderType) []ModelSetting {
	switch providerType {
	case keys.ProviderTypeOpenAi:
		return defaultOpenAIModels()
	case keys.ProviderTypeDeepSeek:
		return DefaultDeepSeekModels()
	}
	panic("unknown Provider type")
}

func defaultOpenAIModels() []ModelSetting {
	return []ModelSetting{
		{
			ID:            "gpt-4o-mini",
			Name:          "GPT-4o Mini",
			API:           "chat-completions",
			ContextWindow: 128_000,
			MaxTokens:     16_000,
			Input:         []keys.InputCapability{keys.InputCapabilityText},
			Reasoning:     keys.ReasoningLow,
		},
	}
}

// DefaultDeepSeekModels returns the standard ProviderTypeDeepSeek model list for OpenAI-compatible usage.
func DefaultDeepSeekModels() []ModelSetting {
	return []ModelSetting{
		{
			ID:            "deepseek-chat",
			Name:          "ProviderTypeDeepSeek Chat",
			API:           "chat-completions",
			ContextWindow: 64_000,
			MaxTokens:     8_192,
			Input:         []keys.InputCapability{keys.InputCapabilityText},
			Reasoning:     keys.ReasoningNone,
		},
		{
			ID:            "deepseek-reasoner",
			Name:          "ProviderTypeDeepSeek Reasoner",
			API:           "chat-completions",
			ContextWindow: 64_000,
			MaxTokens:     8_192,
			Input:         []keys.InputCapability{keys.InputCapabilityText},
			Reasoning:     keys.ReasoningHigh,
		},
	}
}

// Log holds all log-related configuration.
type Log struct {
	// Level is the log verbosity: "error", "warn", "info", "debug". Default "info".
	Level string `mapstructure:"level"`
	// FilePath is the path for log output; all levels are written here. Empty means stderr. Default "~/.acto/acto.log".
	FilePath string `mapstructure:"file_path"`
}

func (s *Log) loadFromEnv() {
	if envLevel := os.Getenv("ACGO_LOG_LEVEL"); envLevel != "" {
		s.Level = envLevel
	}
	if envFile := os.Getenv("ACGO_LOG_FILE_PATH"); envFile != "" {
		s.FilePath = expandHome(envFile)
	}
}

func (s *Log) loadAndInit() {
	// 首先取环境变量中的值
	s.loadFromEnv()
	// 设置初始值
	if s.Level == "" {
		s.Level = "info"
	}
	if s.FilePath == "" {
		home, _ := os.UserHomeDir()
		s.FilePath = filepath.Join(home, ".acto", "acto.log")
	} else if strings.HasPrefix(s.FilePath, "~") {
		s.FilePath = expandHome(s.FilePath)
	}
}

// expandHome replaces leading "~" or "~user" with home directory.
func expandHome(path string) string {
	if path == "" || path[0] != '~' {
		return path
	}
	if len(path) > 1 && path[1] != '/' && path[1] != os.PathSeparator {
		return path // ~user not supported, leave as-is
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[1:])
}
