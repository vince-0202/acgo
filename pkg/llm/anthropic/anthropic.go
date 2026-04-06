package anthropic

//
//import (
//	"bufio"
//	"bytes"
//	"encoding/json"
//	"fmt"
//	"github.com/vince-0202/acgo/pkg/config"
//	"github.com/vince-0202/acgo/pkg/llm"
//	"github.com/vince-0202/acgo/pkg/log"
//	"io"
//	"net/http"
//	"strings"
//)
//
//type Client struct {
//	Settings *config.ProviderSetting
//	provider string
//	baseURL  string
//	apiKey   string
//	models   []llm.Model
//	http     *http.Client
//}
//
//type messageParam struct {
//	Role    string `json:"role"`
//	Content string `json:"content"`
//}
//
//type messagesRequest struct {
//	Model       string         `json:"model"`
//	System      string         `json:"system,omitempty"`
//	Messages    []messageParam `json:"messages"`
//	Temperature *float32       `json:"temperature,omitempty"`
//	MaxTokens   int            `json:"max_tokens,omitempty"`
//	StopSeq     []string       `json:"stop_sequences,omitempty"`
//	Stream      bool           `json:"stream,omitempty"`
//}
//
//type messagesResponse struct {
//	Content []struct {
//		Type string `json:"type"`
//		Text string `json:"text"`
//	} `json:"content"`
//	StopReason string `json:"stop_reason"`
//	Usage      struct {
//		InputTokens  int `json:"input_tokens"`
//		OutputTokens int `json:"output_tokens"`
//	} `json:"usage"`
//}
//
//// NewClient creates an Anthropic native API provider client.
//func NewClient(setting *config.ProviderSetting) llm.Provider {
//	c := &Client{
//		Settings: setting,
//		provider: string(setting.Provider),
//		baseURL:  strings.TrimRight(setting.BaseURL, "/"),
//		apiKey:   setting.ApiKey,
//		models:   make([]llm.Model, 0, len(setting.Models)),
//		http:     &http.Client{},
//	}
//	for _, model := range setting.Models {
//		c.models = append(c.models, llm.Model{
//			ModelSetting: model,
//			Provider:     c.provider,
//		})
//	}
//	return c
//}
//
//func (c *Client) Name() string { return c.provider }
//
//func (c *Client) Models() []llm.Model { return c.models }
//
//func (c *Client) Complete(callCtx llm.Context, model llm.Model, opts *llm.Options) (llm.Message, llm.Usage, error) {
//	if strings.TrimSpace(c.apiKey) == "" {
//		return llm.Message{}, llm.Usage{}, fmt.Errorf("provider api key is not set")
//	}
//	reqBody := buildMessagesRequest(callCtx, model, opts, false)
//	resp, err := c.doMessagesRequest(callCtx, reqBody, false)
//	if err != nil {
//		return llm.Message{}, llm.Usage{}, err
//	}
//	text := anthropicTextFromBlocks(resp.Content)
//	usage := llm.Usage{
//		InputTokens:  resp.Usage.InputTokens,
//		OutputTokens: resp.Usage.OutputTokens,
//		TotalTokens:  resp.Usage.InputTokens + resp.Usage.OutputTokens,
//	}
//	return llm.Message{
//		Role:     llm.RoleAssistant,
//		Provider: c.provider,
//		Content:  []llm.ContentBlock{{Type: "text", Text: text}},
//	}, usage, nil
//}
//
//func (c *Client) Stream(callCtx llm.Context, model llm.Model, opts *llm.Options) (<-chan llm.Event, error) {
//	out := make(chan llm.Event)
//	go func() {
//		defer close(out)
//		if strings.TrimSpace(c.apiKey) == "" {
//			out <- llm.Event{Type: llm.EventError, Error: fmt.Errorf("provider api key is not set")}
//			return
//		}
//
//		reqBody := buildMessagesRequest(callCtx, model, opts, true)
//		log.Debugf("[llm] anthropic stream call model=%s provider=%s messages=%d", model.ID, c.provider, len(callCtx.Messages))
//
//		reqBytes, err := json.Marshal(reqBody)
//		if err != nil {
//			out <- llm.Event{Type: llm.EventError, Error: err}
//			return
//		}
//		url := c.baseURL + "/messages"
//		req, err := http.NewRequestWithContext(callCtx, http.MethodPost, url, bytes.NewReader(reqBytes))
//		if err != nil {
//			out <- llm.Event{Type: llm.EventError, Error: err}
//			return
//		}
//		req.Header.Set("x-api-key", c.apiKey)
//		req.Header.Set("anthropic-version", "2023-06-01")
//		req.Header.Set("content-type", "application/json")
//		req.Header.Set("accept", "text/event-stream")
//
//		resp, err := c.http.Do(req)
//		if err != nil {
//			out <- llm.Event{Type: llm.EventError, Error: err}
//			return
//		}
//		defer resp.Body.Close()
//		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
//			body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
//			out <- llm.Event{Type: llm.EventError, Error: fmt.Errorf("anthropic stream failed: status=%d body=%s", resp.StatusCode, string(body))}
//			return
//		}
//
//		out <- llm.Event{Type: llm.EventStart}
//		textStarted := false
//		var usage llm.Usage
//		stopReason := "stop"
//
//		scanner := bufio.NewScanner(resp.Body)
//		scanner.Buffer(make([]byte, 0, 1024), 1024*1024)
//		var eventType string
//		for scanner.Scan() {
//			line := scanner.Text()
//			if line == "" {
//				eventType = ""
//				continue
//			}
//			if strings.HasPrefix(line, "event:") {
//				eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
//				continue
//			}
//			if !strings.HasPrefix(line, "data:") {
//				continue
//			}
//			raw := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
//			if raw == "" || raw == "[DONE]" {
//				continue
//			}
//
//			switch eventType {
//			case "message_start":
//				var ms struct {
//					Message struct {
//						Usage struct {
//							InputTokens int `json:"input_tokens"`
//						} `json:"usage"`
//					} `json:"message"`
//				}
//				if json.Unmarshal([]byte(raw), &ms) == nil {
//					usage.InputTokens = ms.Message.Usage.InputTokens
//				}
//			case "content_block_delta":
//				var cd struct {
//					Delta struct {
//						Type string `json:"type"`
//						Text string `json:"text"`
//					} `json:"delta"`
//				}
//				if json.Unmarshal([]byte(raw), &cd) == nil && cd.Delta.Type == "text_delta" && cd.Delta.Text != "" {
//					if !textStarted {
//						textStarted = true
//						out <- llm.Event{Type: llm.EventTextStart}
//					}
//					out <- llm.Event{Type: llm.EventTextDelta, TextDelta: cd.Delta.Text}
//				}
//			case "message_delta":
//				var md struct {
//					Delta struct {
//						StopReason string `json:"stop_reason"`
//					} `json:"delta"`
//					Usage struct {
//						OutputTokens int `json:"output_tokens"`
//					} `json:"usage"`
//				}
//				if json.Unmarshal([]byte(raw), &md) == nil {
//					if md.Delta.StopReason != "" {
//						stopReason = md.Delta.StopReason
//					}
//					if md.Usage.OutputTokens > 0 {
//						usage.OutputTokens = md.Usage.OutputTokens
//					}
//				}
//			}
//		}
//		if err := scanner.Err(); err != nil {
//			out <- llm.Event{Type: llm.EventError, Error: err}
//			return
//		}
//
//		if !textStarted {
//			out <- llm.Event{Type: llm.EventTextStart}
//		}
//		out <- llm.Event{Type: llm.EventTextEnd}
//		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
//		out <- llm.Event{Type: llm.EventDone, StopReason: stopReason, Usage: &usage}
//	}()
//	return out, nil
//}
//
//func buildMessagesRequest(callCtx llm.Context, model llm.Model, opts *llm.Options, streaming bool) messagesRequest {
//	system, msgs := convertMessages(callCtx.Messages)
//	req := messagesRequest{
//		Model:    model.ID,
//		System:   system,
//		Messages: msgs,
//		Stream:   streaming,
//	}
//	if opts != nil {
//		if opts.Temperature > 0 {
//			t := opts.Temperature
//			req.Temperature = &t
//		}
//		if opts.MaxOutputTokens > 0 {
//			req.MaxTokens = opts.MaxOutputTokens
//		}
//		if len(opts.StopSequences) > 0 {
//			req.StopSeq = opts.StopSequences
//		}
//	}
//	if req.MaxTokens <= 0 {
//		req.MaxTokens = 1024
//	}
//	return req
//}
//
//func convertMessages(msgs []llm.Message) (string, []messageParam) {
//	var systemParts []string
//	out := make([]messageParam, 0, len(msgs))
//	for _, m := range msgs {
//		text := blocksToText(m.Content)
//		switch m.Role {
//		case llm.RoleSystem:
//			if text != "" {
//				systemParts = append(systemParts, text)
//			}
//		case llm.RoleUser:
//			out = append(out, messageParam{Role: "user", Content: text})
//		case llm.RoleAssistant:
//			out = append(out, messageParam{Role: "assistant", Content: text})
//		case llm.RoleTool:
//			if text != "" {
//				out = append(out, messageParam{
//					Role:    "user",
//					Content: "Tool result:\n" + text,
//				})
//			}
//		}
//	}
//	if len(out) == 0 {
//		out = append(out, messageParam{Role: "user", Content: ""})
//	}
//	return strings.Join(systemParts, "\n\n"), out
//}
//
//func blocksToText(blocks []llm.ContentBlock) string {
//	var b strings.Builder
//	for _, c := range blocks {
//		if c.Type == "text" {
//			b.WriteString(c.Text)
//		}
//	}
//	return b.String()
//}
//
//func anthropicTextFromBlocks(blocks []struct {
//	Type string `json:"type"`
//	Text string `json:"text"`
//}) string {
//	var b strings.Builder
//	for _, p := range blocks {
//		if p.Type == "text" {
//			b.WriteString(p.Text)
//		}
//	}
//	return b.String()
//}
//
//func (c *Client) doMessagesRequest(callCtx llm.Context, body messagesRequest, stream bool) (*messagesResponse, error) {
//	reqBytes, err := json.Marshal(body)
//	if err != nil {
//		return nil, err
//	}
//	url := c.baseURL + "/messages"
//	req, err := http.NewRequestWithContext(callCtx, http.MethodPost, url, bytes.NewReader(reqBytes))
//	if err != nil {
//		return nil, err
//	}
//	req.Header.Set("x-api-key", c.apiKey)
//	req.Header.Set("anthropic-version", "2023-06-01")
//	req.Header.Set("content-type", "application/json")
//	if stream {
//		req.Header.Set("accept", "text/event-stream")
//	}
//	resp, err := c.http.Do(req)
//	if err != nil {
//		return nil, err
//	}
//	defer resp.Body.Close()
//	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
//		b, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
//		return nil, fmt.Errorf("anthropic request failed: status=%d body=%s", resp.StatusCode, string(b))
//	}
//	var out messagesResponse
//	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
//		return nil, err
//	}
//	return &out, nil
//}
