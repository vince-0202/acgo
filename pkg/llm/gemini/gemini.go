package gemini

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/vince-0202/acgo/pkg/communi"
	"github.com/vince-0202/acgo/pkg/config"
	"github.com/vince-0202/acgo/pkg/keys"
	"github.com/vince-0202/acgo/pkg/llm"
	"github.com/vince-0202/acgo/pkg/log"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type Client struct {
	Settings *config.ProviderSetting
	provider string
	baseURL  string
	apiKey   string
	models   []llm.Model
	http     *http.Client
}

type part struct {
	Text string `json:"text"`
}

type content struct {
	Role  string `json:"role,omitempty"`
	Parts []part `json:"parts"`
}

type generationConfig struct {
	Temperature     *float32 `json:"temperature,omitempty"`
	MaxOutputTokens int      `json:"maxOutputTokens,omitempty"`
	StopSequences   []string `json:"stopSequences,omitempty"`
}

type generateContentRequest struct {
	SystemInstruction *content         `json:"systemInstruction,omitempty"`
	Contents          []content        `json:"contents"`
	GenerationConfig  generationConfig `json:"generationConfig,omitempty"`
}

type generateContentResponse struct {
	Candidates []struct {
		Content content `json:"content"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		TotalTokenCount      int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
}

// NewClient creates a Gemini native API provider client.
func NewClient(setting *config.ProviderSetting) llm.Provider {
	c := &Client{
		Settings: setting,
		provider: string(setting.Provider),
		baseURL:  strings.TrimRight(setting.BaseURL, "/"),
		apiKey:   setting.ApiKey,
		models:   make([]llm.Model, 0, len(setting.Models)),
		http:     &http.Client{},
	}
	for _, model := range setting.Models {
		c.models = append(c.models, llm.Model{
			ModelSetting: model,
			Provider:     c.provider,
		})
	}
	return c
}

func (c *Client) Name() string { return c.provider }

func (c *Client) Models() []llm.Model { return c.models }

func (c *Client) Complete(callCtx context.Context, model llm.Model, message []communi.Message, opts *llm.Options) (communi.Message, llm.Usage, error) {
	if strings.TrimSpace(c.apiKey) == "" {
		return communi.Message{}, llm.Usage{}, fmt.Errorf("provider api key is not set")
	}
	reqBody := buildRequest(message, opts)
	respBody, err := c.callGenerate(callCtx, model.ID, reqBody, false)
	if err != nil {
		return communi.Message{}, llm.Usage{}, err
	}
	text := extractResponseText(respBody)
	usage := llm.Usage{
		InputTokens:  respBody.UsageMetadata.PromptTokenCount,
		OutputTokens: respBody.UsageMetadata.CandidatesTokenCount,
		TotalTokens:  respBody.UsageMetadata.TotalTokenCount,
	}
	msg := communi.NewEmptyAssistantMessage()
	msg.AppendMetadata("provider", c.Name())
	msg.AppendTextContent(text)
	return msg, usage, nil
}

func (c *Client) Stream(callCtx context.Context, model llm.Model, message []communi.Message, opts *llm.Options) (<-chan communi.LLMEvent, error) {
	out := make(chan communi.LLMEvent)
	go func() {
		defer close(out)
		if strings.TrimSpace(c.apiKey) == "" {
			out <- communi.LLMEvent{Type: communi.EventError, Error: fmt.Errorf("provider api key is not set")}
			return
		}
		reqBody := buildRequest(message, opts)
		reqBytes, err := json.Marshal(reqBody)
		if err != nil {
			out <- communi.LLMEvent{Type: communi.EventError, Error: err}
			return
		}
		endpoint := c.baseURL + "/v1beta/models/" + url.PathEscape(model.ID) + ":streamGenerateContent?alt=sse&key=" + url.QueryEscape(c.apiKey)
		req, err := http.NewRequestWithContext(callCtx, http.MethodPost, endpoint, bytes.NewReader(reqBytes))
		if err != nil {
			out <- communi.LLMEvent{Type: communi.EventError, Error: err}
			return
		}
		req.Header.Set("content-type", "application/json")
		req.Header.Set("accept", "text/event-stream")

		log.Debugf("[llm] gemini stream call model=%s provider=%s messages=%d", model.ID, c.provider, len(message))
		resp, err := c.http.Do(req)
		if err != nil {
			out <- communi.LLMEvent{Type: communi.EventError, Error: err}
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
			out <- communi.LLMEvent{Type: communi.EventError, Error: fmt.Errorf("gemini stream failed: status=%d body=%s", resp.StatusCode, string(body))}
			return
		}

		out <- communi.LLMEvent{Type: communi.EventStart}
		textStarted := false
		usage := communi.Usage{}
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 1024), 1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "event:") {
				continue
			}
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if payload == "" || payload == "[DONE]" {
				continue
			}
			var chunk generateContentResponse
			if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
				continue
			}
			txt := extractResponseText(&chunk)
			if txt != "" {
				if !textStarted {
					textStarted = true
					out <- communi.LLMEvent{Type: communi.EventTextStart}
				}
				out <- communi.LLMEvent{Type: communi.EventTextDelta, TextDelta: txt}
			}
			if chunk.UsageMetadata.PromptTokenCount > 0 {
				usage.InputTokens = chunk.UsageMetadata.PromptTokenCount
			}
			if chunk.UsageMetadata.CandidatesTokenCount > 0 {
				usage.OutputTokens = chunk.UsageMetadata.CandidatesTokenCount
			}
			if chunk.UsageMetadata.TotalTokenCount > 0 {
				usage.TotalTokens = chunk.UsageMetadata.TotalTokenCount
			}
		}
		if err := scanner.Err(); err != nil {
			out <- communi.LLMEvent{Type: communi.EventError, Error: err}
			return
		}
		if !textStarted {
			out <- communi.LLMEvent{Type: communi.EventTextStart}
		}
		out <- communi.LLMEvent{Type: communi.EventTextEnd}
		if usage.TotalTokens == 0 {
			usage.TotalTokens = usage.InputTokens + usage.OutputTokens
		}
		out <- communi.LLMEvent{Type: communi.EventDone, StopReason: "stop", Usage: &usage}
	}()
	return out, nil
}

func buildRequest(message []communi.Message, opts *llm.Options) generateContentRequest {
	system, contents := convertMessages(message)
	req := generateContentRequest{
		Contents: contents,
	}
	if system != "" {
		req.SystemInstruction = &content{
			Parts: []part{{Text: system}},
		}
	}
	if opts != nil {
		cfg := generationConfig{}
		if opts.Temperature > 0 {
			t := opts.Temperature
			cfg.Temperature = &t
		}
		if opts.MaxOutputTokens > 0 {
			cfg.MaxOutputTokens = opts.MaxOutputTokens
		}
		if len(opts.StopSequences) > 0 {
			cfg.StopSequences = opts.StopSequences
		}
		req.GenerationConfig = cfg
	}
	return req
}

func convertMessages(msgs []communi.Message) (string, []content) {
	var systemParts []string
	out := make([]content, 0, len(msgs))
	for _, m := range msgs {
		text := m.ContentBlocksToText()
		switch m.Role {
		case keys.AgentRoleSystem:
			if text != "" {
				systemParts = append(systemParts, text)
			}
		case keys.AgentRoleUser:
			out = append(out, content{Role: "user", Parts: []part{{Text: text}}})
		case keys.AgentRoleAssistant:
			out = append(out, content{Role: "model", Parts: []part{{Text: text}}})
		case keys.AgentRoleTool:
			if text != "" {
				out = append(out, content{Role: "user", Parts: []part{{Text: "Tool result:\n" + text}}})
			}
		}
	}
	if len(out) == 0 {
		out = append(out, content{Role: "user", Parts: []part{{Text: ""}}})
	}
	return strings.Join(systemParts, "\n\n"), out
}

func extractResponseText(resp *generateContentResponse) string {
	if resp == nil || len(resp.Candidates) == 0 {
		return ""
	}
	var b strings.Builder
	for _, p := range resp.Candidates[0].Content.Parts {
		b.WriteString(p.Text)
	}
	return b.String()
}

func (c *Client) callGenerate(callCtx context.Context, modelID string, body generateContentRequest, stream bool) (*generateContentResponse, error) {
	reqBytes, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	action := ":generateContent"
	if stream {
		action = ":streamGenerateContent?alt=sse"
	}
	sep := "?"
	if strings.Contains(action, "?") {
		sep = "&"
	}
	endpoint := c.baseURL + "/v1beta/models/" + url.PathEscape(modelID) + action + sep + "key=" + url.QueryEscape(c.apiKey)
	req, err := http.NewRequestWithContext(callCtx, http.MethodPost, endpoint, bytes.NewReader(reqBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("content-type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return nil, fmt.Errorf("gemini request failed: status=%d body=%s", resp.StatusCode, string(b))
	}
	var out generateContentResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}
