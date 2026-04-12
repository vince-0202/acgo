package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/vince-0202/acgo/pkg/agent"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/vince-0202/acgo/pkg/communi"
)

const (
	defaultWebFetchTimeout = 20 * time.Second
	defaultWebFetchMaxBody = 64 * 1024
)

type webFetchTool struct {
	client *http.Client
}

func (t *webFetchTool) Name() string  { return "web_fetch" }
func (t *webFetchTool) Label() string { return "Web Fetch" }
func (t *webFetchTool) Description() string {
	return "Fetch text content from a URL using HTTP GET. Use this to read public web pages or API responses."
}

func (t *webFetchTool) JSONSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"minLength":   1,
				"description": "The fully-qualified URL to fetch (http or https).",
			},
			"max_bytes": map[string]any{
				"type":        "number",
				"description": "Maximum bytes to return (default: 65536).",
			},
		},
		"required": []string{"url"},
	}
}

func (t *webFetchTool) Execute(ctx context.Context, toolCallID string, args json.RawMessage, update agent.ToolUpdateFunc) communi.ToolCallResult {
	var params struct {
		URL      string  `json:"url"`
		MaxBytes float64 `json:"max_bytes"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return communi.ErrorToolCallResult(toolCallID, err)
	}
	url := strings.TrimSpace(params.URL)
	if !(strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://")) {
		return communi.ErrorToolCallResult(toolCallID, fmt.Errorf("url must start with http:// or https://"))
	}
	maxBytes := int64(params.MaxBytes)
	if maxBytes <= 0 {
		maxBytes = defaultWebFetchMaxBody
	}

	reqCtx, cancel := context.WithTimeout(ctx, defaultWebFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return communi.ErrorToolCallResult(toolCallID, err)
	}
	req.Header.Set("User-Agent", "acgo-web-fetch/1.0")

	resp, err := t.client.Do(req)
	if err != nil {
		return communi.ErrorToolCallResult(toolCallID, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return communi.ErrorToolCallResult(toolCallID, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return communi.ErrorToolCallResult(toolCallID, fmt.Errorf("web fetch failed: status=%d body=%s", resp.StatusCode, string(body)))
	}

	out := string(body)
	if int64(len(body)) >= maxBytes {
		out += "\n\n(truncated)"
	}
	return communi.NewToolCallResult(toolCallID, out)
}

func NewWebFetchTool() agent.Tool {
	return &webFetchTool{
		client: &http.Client{Timeout: defaultWebFetchTimeout},
	}
}
