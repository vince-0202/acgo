package keys

// InputCapability describes what kind of input a model supports.
// It mirrors the capabilities exposed by pi-ai (e.g. text, image).
type InputCapability string

const (
	InputCapabilityText  InputCapability = "text"
	InputCapabilityImage InputCapability = "image"
)

type ProviderType string

const (
	ProviderTypeUnknown  ProviderType = "unknown"
	ProviderTypeOpenAi                = "openai"
	ProviderTypeDeepSeek              = "deepseek"
)

func GetAPIBaseURL(p ProviderType) ProviderApiBaseUrl {
	switch p {
	case ProviderTypeOpenAi:
		return OpenAiApiBaseUrl
	case ProviderTypeDeepSeek:
		return DeepSeekApiBaseUrl
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
)

func GetProviderTypeByAPIBaseURL(url string) ProviderType {
	switch ProviderApiBaseUrl(url) {
	case OpenAiApiBaseUrl:
		return ProviderTypeOpenAi
	case DeepSeekApiBaseUrl:
		return ProviderTypeDeepSeek
	default:
		return ProviderTypeUnknown
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
