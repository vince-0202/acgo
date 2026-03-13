package keys

// InputCapability describes what kind of input a model supports.
// It mirrors the capabilities exposed by pi-ai (e.g. text, image).
type InputCapability string

const (
	InputCapabilityText  InputCapability = "text"
	InputCapabilityImage InputCapability = "image"
)

// ReasoningCapability describes the qualitative reasoning strength of a model.
// It is intentionally coarse-grained to stay provider-agnostic.
type ReasoningCapability string

const (
	ReasoningNone    ReasoningCapability = "none"
	ReasoningMinimal ReasoningCapability = "minimal"
	ReasoningLow     ReasoningCapability = "low"
	ReasoningMedium  ReasoningCapability = "medium"
	ReasoningHigh    ReasoningCapability = "high"
	ReasoningXHigh   ReasoningCapability = "xhigh"
)

type ProviderType string

const (
	ProviderTypeUnknown  ProviderType = "unknown"
	ProviderTypeOpenAi                = "openai"
	ProviderTypeDeepSeek              = "deepseek"
)

func GetAPIBaseURL(p ProviderType) PROVIDER_API_BASE_URL {
	switch p {
	case ProviderTypeOpenAi:
		return OpenAiAPIBaseURL
	case ProviderTypeDeepSeek:
		return DeepSeekAPIBaseURL
	default:
		return ""
	}
}

type PROVIDER_API_BASE_URL string

const (
	// OpenAiAPIBaseURL  is the default base URL for ProviderTypeOpenAi's API.
	OpenAiAPIBaseURL PROVIDER_API_BASE_URL = "https://api.openai.com/v1"
	// DeepSeekAPIBaseURL is the default base URL for ProviderTypeDeepSeek's OpenAI-compatible API.
	DeepSeekAPIBaseURL PROVIDER_API_BASE_URL = "https://api.deepseek.com"
)

func GetProviderTypeByAPIBaseURL(url string) ProviderType {
	switch PROVIDER_API_BASE_URL(url) {
	case OpenAiAPIBaseURL:
		return ProviderTypeOpenAi
	case DeepSeekAPIBaseURL:
		return ProviderTypeDeepSeek
	default:
		return ProviderTypeUnknown
	}
}
