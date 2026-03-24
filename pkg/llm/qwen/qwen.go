package qwen

import (
	"github.com/vince-0202/acgo/pkg/config"
	"github.com/vince-0202/acgo/pkg/llm"
	"github.com/vince-0202/acgo/pkg/llm/openai"
)

// Client wraps OpenAI-compatible client for 阿里云百炼千问 (DashScope compatible-mode).
// API 文档: https://help.aliyun.com/zh/model-studio/qwen-api-via-openai-chat-completions
type Client openai.Client

// NewClient creates a Qwen provider using the OpenAI-compatible Chat Completions API.
// BaseURL 默认: https://dashscope.aliyuncs.com/compatible-mode/v1 (华北2 北京)。
// API Key 环境变量建议: DASHSCOPE_API_KEY（百炼控制台获取）。
func NewClient(setting *config.ProviderSetting) llm.Provider {
	return openai.NewClient(setting)
}
