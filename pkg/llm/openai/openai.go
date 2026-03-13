package openai

import (
	"acgo/pkg/config"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"acgo/pkg/llm"
	"acgo/pkg/log"
)

const maxLogBodyLen = 8192

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n... (truncated)"
}

// Client is a minimal OpenAI-compatible client implementing the llm.Provider interface.
// It can be used for OpenAI, ProviderTypeDeepSeek, or any HTTP API that follows the OpenAI Chat Completions shape.
type Client struct {
	Settings   *config.ProviderSetting
	provider   string // e.g. "openai"
	baseURL    string
	apiKey     string
	models     []llm.Model
	httpClient *http.Client
}

// ClientOption configures a Client.
type ClientOption func(*Client)

// NewClient creates a new OpenAI-compatible client. baseURL is the API root (e.g. https://api.openai.com/v1).
// If baseURL is empty, it defaults to OpenAI. Options can override provider name, API key env, and models.
func NewClient(setting *config.ProviderSetting, opts ...ClientOption) llm.Provider {
	c := &Client{
		Settings: setting,
		provider: string(setting.Provider),
		baseURL:  setting.BaseURL,
		apiKey:   setting.ApiKey,
		models:   make([]llm.Model, 0, len(setting.Models)),
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
	for _, o := range opts {
		o(c)
	}
	// Ensure each model has Provider set to this client's name.
	for _, model := range setting.Models {
		c.models = append(c.models, llm.Model{
			ModelSetting: model,
			Provider:     c.provider,
		})
	}
	return c
}

func (c *Client) Name() string {
	return c.provider
}

// Models returns the list of models for this provider.
func (c *Client) Models() []llm.Model {
	return c.models
}

// chatCompletionRequest mirrors the OpenAI Chat Completions API shape (simplified).
type chatCompletionRequest struct {
	Model       string         `json:"model"`
	Messages    []chatMessage  `json:"messages"`
	Stream      bool           `json:"stream"`
	MaxTokens   int            `json:"max_tokens,omitempty"`
	Temperature float32        `json:"temperature,omitempty"`
	Stop        []string       `json:"stop,omitempty"`
	Tools       []openAITool   `json:"tools,omitempty"`
	ToolChoice  any            `json:"tool_choice,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// chatMessage: for tool role, Content must be a string and tool_call_id set; for other roles, Content is []contentPart.
// For assistant with tool use, ToolCalls must be set and Content must be []contentPart (each part with "text" for type "text").
type chatMessage struct {
	Role       string           `json:"role"`
	Content    interface{}      `json:"content"` // string for "tool", []contentPart for user/assistant/system
	ToolCallID string           `json:"tool_call_id,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
}

// openAIToolCall is the assistant-message tool call shape for the API.
type openAIToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"` // "function"
	Function openAIToolCallFn `json:"function"`
}

type openAIToolCallFn struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type contentPart struct {
	Type string `json:"type"`
	Text string `json:"text"` // must be present for API (no omitempty)
}

type chatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			ToolCalls []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string          `json:"name"`
					Arguments json.RawMessage `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

type openAITool struct {
	Type     string         `json:"type"` // always "function"
	Function openAIFunction `json:"function"`
}

type openAIFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// Stream implements llm.Provider.Stream.
// For the MVP we implement streaming by internally performing a non-streaming call
// and emitting a small sequence of llm.Event values.
func (c *Client) Stream(ctx context.Context, model llm.Model, context llm.Context, opts *llm.Options) (<-chan llm.Event, error) {
	out := make(chan llm.Event)
	go func() {
		defer close(out)

		msg, _, err := c.Complete(ctx, model, context, opts)
		if err != nil {
			out <- llm.Event{Type: llm.EventError, Error: err}
			return
		}
		out <- llm.Event{Type: llm.EventStart}

		// If the model requested a tool, emit toolcall events instead of text.
		if msg.ToolCall != nil {
			out <- llm.Event{Type: llm.EventToolCallStart, ToolCall: msg.ToolCall}
			out <- llm.Event{Type: llm.EventToolCallEnd, ToolCall: msg.ToolCall}
			out <- llm.Event{Type: llm.EventDone, StopReason: "toolUse"}
			return
		}

		out <- llm.Event{Type: llm.EventTextStart}
		// Emit the full content as a single delta for now.
		var fullText string
		for _, block := range msg.Content {
			if block.Type == "text" {
				fullText += block.Text
			}
		}
		if fullText != "" {
			out <- llm.Event{Type: llm.EventTextDelta, TextDelta: fullText}
		}
		out <- llm.Event{Type: llm.EventTextEnd}
		out <- llm.Event{Type: llm.EventDone, StopReason: "stop"}
	}()
	return out, nil
}

// Complete implements llm.Provider.Complete using the Chat Completions API.
func (c *Client) Complete(ctx context.Context, model llm.Model, context llm.Context, opts *llm.Options) (llm.Message, llm.Usage, error) {
	if c.apiKey == "" {
		return llm.Message{}, llm.Usage{}, fmt.Errorf("%s is not set", c.apiKey)
	}

	log.Debugf("[llm] call model=%s provider=%s messages=%d", model.ID, c.provider, len(context.Messages))
	if opts != nil && len(opts.Tools) > 0 {
		log.Debugf("[llm] tools=%d names=%v", len(opts.Tools), toolNames(opts.Tools))
	}

	reqBody := chatCompletionRequest{
		Model:    model.ID,
		Messages: convertMessages(context.Messages),
		Stream:   false,
	}
	if opts != nil {
		reqBody.Temperature = opts.Temperature
		reqBody.MaxTokens = opts.MaxOutputTokens
		reqBody.Stop = opts.StopSequences
		if len(opts.Tools) > 0 {
			reqBody.Tools = convertTools(opts.Tools)
			// Let the model decide when to use tools by default.
			if opts.ToolChoice != "" {
				reqBody.ToolChoice = opts.ToolChoice
			}
		}
		reqBody.Metadata = opts.Metadata
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return llm.Message{}, llm.Usage{}, err
	}
	log.Debugf("[llm] request body:\n%s", truncate(string(data), maxLogBodyLen))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return llm.Message{}, llm.Usage{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		log.Debugf("[llm] request error: %v", err)
		return llm.Message{}, llm.Usage{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Debugf("[llm] read body error: %v", err)
		return llm.Message{}, llm.Usage{}, err
	}
	log.Debugf("[llm] response status=%d body:\n%s", resp.StatusCode, truncate(string(body), maxLogBodyLen))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr struct {
			Error struct {
				Message string `json:"message"`
				Type    string `json:"type"`
				Code    string `json:"code"`
			} `json:"error"`
		}
		_ = json.Unmarshal(body, &apiErr)
		if apiErr.Error.Message != "" {
			log.Debugf("[llm] API error: %d %s", resp.StatusCode, apiErr.Error.Message)
			return llm.Message{}, llm.Usage{}, fmt.Errorf("openai API %d: %s", resp.StatusCode, apiErr.Error.Message)
		}
		log.Debugf("[llm] API error: %d %s", resp.StatusCode, string(body))
		return llm.Message{}, llm.Usage{}, fmt.Errorf("openai API %d: %s", resp.StatusCode, string(body))
	}

	var raw chatCompletionResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return llm.Message{}, llm.Usage{}, err
	}
	if len(raw.Choices) == 0 {
		return llm.Message{}, llm.Usage{}, fmt.Errorf("no choices returned from API (model may have returned empty or filtered response)")
	}

	choice := raw.Choices[0]
	msg := llm.Message{
		Role:     llm.RoleAssistant,
		Provider: c.Name(),
	}
	// If the model responded with tool calls, map the first one to llm.ToolCall.
	if len(choice.Message.ToolCalls) > 0 {
		tc := choice.Message.ToolCalls[0]
		msg.ToolCall = &llm.ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		}
	} else {
		msg.Content = []llm.ContentBlock{
			{Type: "text", Text: choice.Message.Content},
		}
	}
	usage := llm.Usage{
		InputTokens:  raw.Usage.PromptTokens,
		OutputTokens: raw.Usage.CompletionTokens,
	}
	log.Debugf("[llm] return usage in=%d out=%d", usage.InputTokens, usage.OutputTokens)
	if msg.ToolCall != nil {
		log.Debugf("[llm] return tool_call id=%s name=%s args=%s", msg.ToolCall.ID, msg.ToolCall.Name, truncate(string(msg.ToolCall.Arguments), 1024))
	} else {
		var contentPreview string
		for _, b := range msg.Content {
			if b.Type == "text" {
				contentPreview += b.Text
			}
		}
		log.Debugf("[llm] return content:\n%s", truncate(contentPreview, 1024))
	}
	return msg, usage, nil
}

func toolNames(tools []llm.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.Name)
	}
	return names
}

func convertTools(tools []llm.Tool) []openAITool {
	out := make([]openAITool, 0, len(tools))
	for _, t := range tools {
		out = append(out, openAITool{
			Type: "function",
			Function: openAIFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.JSONSchema,
			},
		})
	}
	return out
}

func convertMessages(msgs []llm.Message) []chatMessage {
	out := make([]chatMessage, 0, len(msgs))
	for _, m := range msgs {
		role := string(m.Role)
		if role == "tool" {
			var text string
			for _, c := range m.Content {
				if c.Type == "text" {
					text += c.Text
				}
			}
			out = append(out, chatMessage{
				Role:       role,
				Content:    text,
				ToolCallID: m.ToolCallID,
			})
			continue
		}
		parts := make([]contentPart, 0, len(m.Content))
		for _, c := range m.Content {
			if c.Type == "text" {
				parts = append(parts, contentPart{Type: "text", Text: c.Text})
			}
		}
		// OpenAI/ProviderTypeDeepSeek require assistant content to be an array of parts each with "text" when type is "text".
		// When the message has a tool call and no text, use at least one part with empty text so the field is present.
		if role == "assistant" && m.ToolCall != nil && len(parts) == 0 {
			parts = []contentPart{{Type: "text", Text: ""}}
		}
		cm := chatMessage{Role: role, Content: parts}
		if role == "assistant" && m.ToolCall != nil {
			cm.ToolCalls = []openAIToolCall{{
				ID:       m.ToolCall.ID,
				Type:     "function",
				Function: openAIToolCallFn{Name: m.ToolCall.Name, Arguments: m.ToolCall.Arguments},
			}}
		}
		out = append(out, cm)
	}
	return out
}
