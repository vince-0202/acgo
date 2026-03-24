package config

import (
	"github.com/vince-0202/acgo/pkg/keys"
	"os"
	"path/filepath"
	"strings"
)

// Settings represents the merged configuration for acgo.
type Settings struct {
	WorkDir string
	Log     Log           `mapstructure:"log"`
	Agent   AgentSetting  `mapstructure:"agent"`
	Session SessionConfig `mapstructure:"session"`
	Rag     RagSetting    `mapstructure:"rag"`
}

// SessionConfig holds session storage settings.
type SessionConfig struct {
	// Root is the directory for session JSONL files (default: ~/.acgo/sessions).
	Root string `mapstructure:"root"`
}

func (s *SessionConfig) LoadAndInit(globalDir string) {
	if s.Root == "" {
		s.Root = filepath.Join(globalDir, "sessions")
	} else if s.Root[0] == '~' {
		s.Root = expandHome(s.Root)
	}
}

func NewDefaultProviderSettingByProvider(providerType keys.ProviderType) *ProviderSetting {
	switch providerType {
	case keys.ProviderTypeOpenAi:
		return newDefaultOpenaiProviderSetting()
	case keys.ProviderTypeDeepSeek:
		return newDefaultDeepseekProviderSetting()
	case keys.ProviderTypeQwen:
		return newDefaultQwenProviderSetting()
	default:
		panic("invalid Provider type")
	}
}

type ProviderSetting struct {
	Provider keys.ProviderType `mapstructure:"provider"` // e.g. "openai"
	BaseURL  string            `mapstructure:"base_url"`
	ApiKey   string            `mapstructure:"api_key"` // env var name for API key, e.g. "OPENAI_API_KEY", "DEEPSEEK_API_KEY"
	Models   []ModelSetting    `mapstructure:"Models"`
}

func (s *ProviderSetting) Init() {
	if s.BaseURL == "" {
		s.BaseURL = string(keys.GetAPIBaseURL(s.Provider))
	}
	if len(s.Models) == 0 {
		s.Models = NewDefaultModelsByProvider(s.Provider)
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

func newDefaultQwenProviderSetting() *ProviderSetting {
	var qwen keys.ProviderType = keys.ProviderTypeQwen
	return &ProviderSetting{
		Provider: qwen,
		BaseURL:  string(keys.GetAPIBaseURL(qwen)),
		ApiKey:   "DASHSCOPE_API_KEY", // 百炼控制台 API Key，见 https://bailian.console.aliyun.com
		Models:   NewDefaultModelsByProvider(qwen),
	}
}

type ModelSetting struct {
	ID            string                 `mapstructure:"id"`             // unique identifier within Provider
	Name          string                 `mapstructure:"name"`           // human readable name
	API           string                 `mapstructure:"api"`            // underlying API family (e.g. openai-chat, openai-responses)
	ContextWindow int                    `mapstructure:"context_window"` // approximate context window in tokens
	MaxTokens     int                    `mapstructure:"max_tokens"`     // maximum generation tokens
	Input         []keys.InputCapability `mapstructure:"input"`          // supported input modalities
	Reasoning     keys.ThinkingLevel     `mapstructure:"reasoning"`      // reasoning capability level
}

func NewDefaultModelsByProvider(providerType keys.ProviderType) []ModelSetting {
	switch providerType {
	case keys.ProviderTypeOpenAi:
		return defaultOpenAIModels()
	case keys.ProviderTypeDeepSeek:
		return DefaultDeepSeekModels()
	case keys.ProviderTypeQwen:
		return DefaultQwenModels()
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
			Reasoning:     keys.ThinkingLow,
		},
	}
}

// DefaultDeepSeekModels returns the standard ProviderTypeDeepSeek model list for OpenAI-compatible usage.
func DefaultDeepSeekModels() []ModelSetting {
	return []ModelSetting{
		{
			ID:            "deepseek-reasoner",
			Name:          "ProviderTypeDeepSeek Reasoner",
			API:           "chat-completions",
			ContextWindow: 64_000,
			MaxTokens:     8_192,
			Input:         []keys.InputCapability{keys.InputCapabilityText},
			Reasoning:     keys.ThinkingHigh,
		},
		{
			ID:            "deepseek-chat",
			Name:          "ProviderTypeDeepSeek Chat",
			API:           "chat-completions",
			ContextWindow: 64_000,
			MaxTokens:     8_192,
			Input:         []keys.InputCapability{keys.InputCapabilityText},
			Reasoning:     keys.ThinkingNone,
		},
	}
}

// DefaultQwenModels returns the standard 阿里云百炼千问 model list (OpenAI compatible).
// 模型列表: https://help.aliyun.com/zh/model-studio/getting-started/models
func DefaultQwenModels() []ModelSetting {
	return []ModelSetting{
		{
			ID:            "qwen-plus",
			Name:          "千问 Plus",
			API:           "chat-completions",
			ContextWindow: 128_000,
			MaxTokens:     8_192,
			Input:         []keys.InputCapability{keys.InputCapabilityText},
			Reasoning:     keys.ThinkingLow,
		},
		{
			ID:            "qwen-turbo",
			Name:          "千问 Turbo",
			API:           "chat-completions",
			ContextWindow: 128_000,
			MaxTokens:     6_000,
			Input:         []keys.InputCapability{keys.InputCapabilityText},
			Reasoning:     keys.ThinkingNone,
		},
		{
			ID:            "qwen-max",
			Name:          "千问 Max",
			API:           "chat-completions",
			ContextWindow: 32_768,
			MaxTokens:     8_192,
			Input:         []keys.InputCapability{keys.InputCapabilityText},
			Reasoning:     keys.ThinkingMedium,
		},
		// Embedding 模型，便于在配置中直接选择 text-embedding-v3 作为 default_embedding_model。
		{
			ID:            keys.DefaultQwenEmbeddingModel,
			Name:          "千问 Text Embedding v3",
			API:           "embeddings",
			ContextWindow: 8_192,
			MaxTokens:     8_192,
			Input:         []keys.InputCapability{keys.InputCapabilityText},
			Reasoning:     keys.ThinkingNone,
		},
	}
}

// Log holds all log-related configuration.
type Log struct {
	// Level is the log verbosity: "error", "warn", "info", "debug". Default "info".
	Level string `mapstructure:"level"`
	// FilePath is the path for log output; all levels are written here. Empty means stderr. Default "~/.acgo/acgo.log".
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

func (s *Log) LoadAndInit() {
	// 首先取环境变量中的值
	s.loadFromEnv()
	// 设置初始值
	if s.Level == "" {
		s.Level = "info"
	}
	if s.FilePath == "" {
		home, _ := os.UserHomeDir()
		s.FilePath = filepath.Join(home, ".acgo", "acgo.log")
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
