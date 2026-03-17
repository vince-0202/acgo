package qwen

import (
	"acgo/pkg/keys"
	"acgo/pkg/llm/openai"
	"net/http"
)

// NewEmbeddingClient creates an embedding client for 阿里云百炼向量化接口（OpenAI 兼容）.
// baseURL 应为 https://dashscope.aliyuncs.com/compatible-mode/v1（华北2）或其它地域；
// apiKey 为百炼 API Key（或环境变量名，由调用方解析）.
// 默认使用 text-embedding-v3，文档: https://help.aliyun.com/zh/model-studio/developer-reference/embedding-interfaces-compatible-with-openai
func NewEmbeddingClient(baseURL, apiKey string, httpClient *http.Client) *openai.EmbeddingClient {
	return openai.NewEmbeddingClient(baseURL, apiKey, keys.DefaultQwenEmbeddingModel, httpClient)
}
