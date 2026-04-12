package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/vince-0202/acgo/pkg/agent"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/vince-0202/acgo/pkg/communi"
)

const (
	defaultWebSearchTimeout = 20 * time.Second
	defaultWebSearchLimit   = 5
)

type webSearchTool struct {
	client *http.Client
}

type duckDuckGoResponse struct {
	AbstractText  string `json:"AbstractText"`
	AbstractURL   string `json:"AbstractURL"`
	RelatedTopics []struct {
		Text     string `json:"Text"`
		FirstURL string `json:"FirstURL"`
		Topics   []struct {
			Text     string `json:"Text"`
			FirstURL string `json:"FirstURL"`
		} `json:"Topics"`
	} `json:"RelatedTopics"`
}

func (t *webSearchTool) Name() string  { return "web_search" }
func (t *webSearchTool) Label() string { return "Web Search" }
func (t *webSearchTool) Description() string {
	return "Search the web and return top results with title/snippet URLs."
}

func (t *webSearchTool) JSONSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"minLength":   1,
				"description": "Search query text.",
			},
			"max_results": map[string]any{
				"type":        "number",
				"description": "Maximum number of results to return (default: 5).",
			},
		},
		"required": []string{"query"},
	}
}

func (t *webSearchTool) Execute(ctx context.Context, toolCallID string, args json.RawMessage, update agent.ToolUpdateFunc) communi.ToolCallResult {
	var params struct {
		Query      string  `json:"query"`
		MaxResults float64 `json:"max_results"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return communi.ErrorToolCallResult(toolCallID, err)
	}
	query := strings.TrimSpace(params.Query)
	if query == "" {
		return communi.ErrorToolCallResult(toolCallID, fmt.Errorf("query is required"))
	}
	maxResults := int(params.MaxResults)
	if maxResults <= 0 {
		maxResults = defaultWebSearchLimit
	}

	ddgURL := "https://api.duckduckgo.com/?format=json&no_html=1&skip_disambig=1&q=" + url.QueryEscape(query)
	reqCtx, cancel := context.WithTimeout(ctx, defaultWebSearchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, ddgURL, nil)
	if err != nil {
		return communi.ErrorToolCallResult(toolCallID, err)
	}
	req.Header.Set("User-Agent", "acgo-web-search/1.0")

	resp, err := t.client.Do(req)
	if err != nil {
		return communi.ErrorToolCallResult(toolCallID, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return communi.ErrorToolCallResult(toolCallID, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return communi.ErrorToolCallResult(toolCallID, fmt.Errorf("web search failed: status=%d body=%s", resp.StatusCode, string(body)))
	}

	var parsed duckDuckGoResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return communi.ErrorToolCallResult(toolCallID, err)
	}

	type result struct {
		Text string
		URL  string
	}
	results := make([]result, 0, maxResults)
	if strings.TrimSpace(parsed.AbstractText) != "" || strings.TrimSpace(parsed.AbstractURL) != "" {
		results = append(results, result{Text: parsed.AbstractText, URL: parsed.AbstractURL})
	}
	for _, topic := range parsed.RelatedTopics {
		if len(results) >= maxResults {
			break
		}
		if strings.TrimSpace(topic.Text) != "" || strings.TrimSpace(topic.FirstURL) != "" {
			results = append(results, result{Text: topic.Text, URL: topic.FirstURL})
			continue
		}
		for _, nested := range topic.Topics {
			if len(results) >= maxResults {
				break
			}
			if strings.TrimSpace(nested.Text) == "" && strings.TrimSpace(nested.FirstURL) == "" {
				continue
			}
			results = append(results, result{Text: nested.Text, URL: nested.FirstURL})
		}
	}

	if len(results) == 0 {
		return communi.NewToolCallResult(toolCallID, "no web search results found")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "query: %s\n\n", query)
	for i, r := range results {
		fmt.Fprintf(&b, "[%d] %s\n%s\n\n", i+1, strings.TrimSpace(r.Text), strings.TrimSpace(r.URL))
	}
	return communi.NewToolCallResult(toolCallID, strings.TrimSpace(b.String()))
}

func NewWebSearchTool() agent.Tool {
	return &webSearchTool{
		client: &http.Client{Timeout: defaultWebSearchTimeout},
	}
}
