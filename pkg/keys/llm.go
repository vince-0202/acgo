package keys

import (
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

// InputCapability describes what kind of input a model supports.
// It mirrors the capabilities exposed by pi-ai (e.g. text, image).
type InputCapability string

const (
	InputCapabilityText  InputCapability = "text"
	InputCapabilityImage InputCapability = "image"
)

type ProviderType string

const (
	ProviderTypeUnknown   ProviderType = "unknown"
	ProviderTypeOpenAi                 = "openai"
	ProviderTypeDeepSeek               = "deepseek"
	ProviderTypeQwen      ProviderType = "qwen" // 阿里云百炼千问，OpenAI 兼容 API
	ProviderTypeAnthropic              = "anthropic"
	ProviderTypeGemini                 = "gemini"
	ProviderTypeGLM                    = "glm" // 智谱 GLM，OpenAI 兼容 API
)

func GetAPIBaseURL(p ProviderType) ProviderApiBaseUrl {
	switch p {
	case ProviderTypeOpenAi:
		return OpenAiApiBaseUrl
	case ProviderTypeDeepSeek:
		return DeepSeekApiBaseUrl
	case ProviderTypeQwen:
		return QwenApiBaseUrl
	case ProviderTypeAnthropic:
		return AnthropicApiBaseUrl
	case ProviderTypeGemini:
		return GeminiApiBaseUrl
	case ProviderTypeGLM:
		return GLMApiBaseUrl
	default:
		return ""
	}
}

type ProviderApiBaseUrl string

const (
	// OpenAiApiBaseUrl  is the default base URL for ProviderTypeOpenAi's API.
	OpenAiApiBaseUrl ProviderApiBaseUrl = "https://api.openai.com/v1"
	// DeepSeekApiBaseUrl is the default base URL for ProviderTypeDeepSeek's OpenAI-compatible API.
	DeepSeekApiBaseUrl ProviderApiBaseUrl = "https://api.deepseek.com"
	// QwenApiBaseUrl is the default base URL for 阿里云百炼千问 (DashScope compatible-mode, 华北2 北京).
	// 其他地域: 新加坡 https://dashscope-intl.aliyuncs.com/compatible-mode/v1
	// 美国弗吉尼亚: https://dashscope-us.aliyuncs.com/compatible-mode/v1
	QwenApiBaseUrl ProviderApiBaseUrl = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	// AnthropicApiBaseUrl is the default Anthropic API root.
	AnthropicApiBaseUrl ProviderApiBaseUrl = "https://api.anthropic.com/v1"
	// GeminiApiBaseUrl is the native Gemini API host.
	GeminiApiBaseUrl ProviderApiBaseUrl = "https://generativelanguage.googleapis.com"
	// GLMApiBaseUrl is the default base URL for Zhipu GLM OpenAI-compatible API.
	GLMApiBaseUrl ProviderApiBaseUrl = "https://open.bigmodel.cn/api/paas/v4"
)

func GetProviderTypeByAPIBaseURL(url string) ProviderType {
	switch ProviderApiBaseUrl(url) {
	case OpenAiApiBaseUrl:
		return ProviderTypeOpenAi
	case DeepSeekApiBaseUrl:
		return ProviderTypeDeepSeek
	case QwenApiBaseUrl:
		return ProviderTypeQwen
	case AnthropicApiBaseUrl:
		return ProviderTypeAnthropic
	case GeminiApiBaseUrl:
		return ProviderTypeGemini
	case GLMApiBaseUrl:
		return ProviderTypeGLM
	default:
		return ProviderTypeUnknown
	}
}

// Default embedding model IDs for RAG (OpenAI-compatible /embeddings endpoint).
// 百炼文档: https://help.aliyun.com/zh/model-studio/developer-reference/embedding-interfaces-compatible-with-openai
const (
	// DefaultQwenEmbeddingModel 百炼默认文本向量模型，支持 dimensions 等参数
	DefaultQwenEmbeddingModel = "text-embedding-v3"
)

// GetDefaultEmbeddingModel returns the embedding model ID for the provider when doing RAG.
// If the provider has a dedicated embedding model (e.g. Qwen text-embedding-v3), return it;
// otherwise return "" and the caller should use the chat default model.
func GetDefaultEmbeddingModel(p ProviderType) string {
	switch p {
	case ProviderTypeQwen:
		return DefaultQwenEmbeddingModel
	default:
		return ""
	}
}

// ThinkingLevel controls reasoning intensity, similar to pi-agent-core.
type ThinkingLevel string

const (
	ThinkingNone    ThinkingLevel = "none"
	ThinkingMinimal ThinkingLevel = "minimal"
	ThinkingLow     ThinkingLevel = "low"
	ThinkingMedium  ThinkingLevel = "medium"
	ThinkingHigh    ThinkingLevel = "high"
	ThinkingXHigh   ThinkingLevel = "xhigh"
)

func MapReasoningEffort(level ThinkingLevel) shared.ReasoningEffort {
	switch level {
	case ThinkingMinimal:
		return openai.ReasoningEffortMinimal
	case ThinkingLow:
		return openai.ReasoningEffortLow
	case ThinkingMedium:
		return openai.ReasoningEffortMedium
	case ThinkingHigh:
		return openai.ReasoningEffortHigh
	case ThinkingXHigh:
		return openai.ReasoningEffortXhigh
	case ThinkingNone:
		return openai.ReasoningEffortNone
	default:
		return openai.ReasoningEffortNone
	}
}
