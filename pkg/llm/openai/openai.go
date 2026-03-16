package openai

import (
	"acgo/pkg/config"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"acgo/pkg/keys"
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

// deepSeekThinking enables DeepSeek thinking mode per https://api-docs.deepseek.com/zh-cn/guides/thinking_mode
type deepSeekThinking struct {
	Type string `json:"type"` // "enabled"
}

// chatCompletionRequest mirrors the OpenAI Chat Completions API shape (simplified).
type chatCompletionRequest struct {
	Model       string            `json:"model"`
	Messages    []chatMessage     `json:"messages"`
	Stream      bool              `json:"stream"`
	MaxTokens   int               `json:"max_tokens,omitempty"`
	Temperature float32           `json:"temperature,omitempty"`
	Stop        []string          `json:"stop,omitempty"`
	Tools       []openAITool      `json:"tools,omitempty"`
	ToolChoice  any               `json:"tool_choice,omitempty"`
	Metadata    map[string]any    `json:"metadata,omitempty"`
	Thinking    *deepSeekThinking `json:"thinking,omitempty"` // DeepSeek only: enable reasoning_content
}

// chatMessage: for tool role, Content must be a string and tool_call_id set; for other roles, Content is []contentPart.
// For assistant with tool use, ToolCalls must be set and Content must be []contentPart (each part with "text" for type "text").
// ReasoningContent: DeepSeek thinking mode requires this field on every assistant message (use "" when none).
// No omitempty so the field is always sent for assistant; OpenAI ignores unknown fields.
type chatMessage struct {
	Role             string           `json:"role"`
	Content          interface{}      `json:"content"`           // string for "tool", []contentPart for user/assistant/system
	ReasoningContent string           `json:"reasoning_content"` // required by DeepSeek thinking mode for assistant; empty for user/system/tool
	ToolCallID       string           `json:"tool_call_id,omitempty"`
	ToolCalls        []openAIToolCall `json:"tool_calls,omitempty"`
}

// openAIToolCall is the assistant-message tool call shape for the API.
type openAIToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"` // "function"
	Function openAIToolCallFn `json:"function"`
}

type openAIToolCallFn struct {
	Name      string            `json:"name"`
	Arguments argumentsAsString `json:"arguments"` // API expects a string; we store raw JSON and marshal as string
}

// argumentsAsString marshals as a JSON string so the request body has "arguments": "{\"key\":\"val\"}".
// OpenAI/DeepSeek require tool_calls[].function.arguments to be a string, not an object.
type argumentsAsString json.RawMessage

func (a argumentsAsString) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(a))
}

type contentPart struct {
	Type string `json:"type"`
	Text string `json:"text"` // must be present for API (no omitempty)
}

type chatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Role             string `json:"role"`
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"` // DeepSeek thinking mode
			ToolCalls        []struct {
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

// streamChunk is one SSE data item from chat/completions with stream=true.
type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"` // DeepSeek
			ToolCalls        []struct {
				Index    int `json:"index"`
				ID       string
				Type     string
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

type openAIFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// Stream implements llm.Provider.Stream using real SSE when the API supports it.
// For models that return reasoning_content (e.g. DeepSeek), EventThinkingStart/Delta/End are emitted as chunks arrive.
func (c *Client) Stream(ctx context.Context, model llm.Model, llmCtx llm.Context, opts *llm.Options) (<-chan llm.Event, error) {
	out := make(chan llm.Event)
	go func() {
		defer close(out)
		c.streamSSE(ctx, model, llmCtx, opts, out)
	}()
	return out, nil
}

func (c *Client) streamSSE(ctx context.Context, model llm.Model, llmCtx llm.Context, opts *llm.Options, out chan<- llm.Event) {
	body, err := c.buildStreamRequest(model, llmCtx, opts)
	if err != nil {
		out <- llm.Event{Type: llm.EventError, Error: err}
		return
	}
	resp, err := c.doStreamHTTP(ctx, body, out)
	if err != nil {
		out <- llm.Event{Type: llm.EventError, Error: err}
		return
	}
	if resp == nil {
		return
	}
	defer resp.Body.Close()
	out <- llm.Event{Type: llm.EventStart}

	state := &sseStreamState{}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(nil, 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			break
		}
		var chunk streamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil || len(chunk.Choices) == 0 {
			continue
		}
		if state.processChunk(&chunk, out) {
			return
		}
	}
	state.emitEnd(out)
	if err := sc.Err(); err != nil {
		out <- llm.Event{Type: llm.EventError, Error: err}
		return
	}
	out <- llm.Event{Type: llm.EventDone, StopReason: "stop", Usage: &state.usage}
}

// buildStreamRequest builds the request body for a streaming chat completion.
func (c *Client) buildStreamRequest(model llm.Model, llmCtx llm.Context, opts *llm.Options) ([]byte, error) {
	reqBody := chatCompletionRequest{
		Model:    model.ID,
		Messages: convertMessages(llmCtx.Messages),
		Stream:   true,
	}
	if opts != nil {
		reqBody.Temperature = opts.Temperature
		reqBody.MaxTokens = opts.MaxOutputTokens
		reqBody.Stop = opts.StopSequences
		if len(opts.Tools) > 0 {
			reqBody.Tools = convertTools(opts.Tools)
			if opts.ToolChoice != "" {
				reqBody.ToolChoice = opts.ToolChoice
			}
		}
		reqBody.Metadata = opts.Metadata
		if model.Reasoning != keys.ThinkingNone || opts.ReasoningEffort != keys.ThinkingNone {
			reqBody.Thinking = &deepSeekThinking{Type: "enabled"}
		}
	}
	return json.Marshal(reqBody)
}

// doStreamHTTP performs the HTTP request for streaming. On non-2xx it emits EventError and returns (nil, nil).
func (c *Client) doStreamHTTP(ctx context.Context, body []byte, out chan<- llm.Event) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set(keys.ContentType, "application/json")
	req.Header.Set(keys.Authorization, "Bearer "+c.apiKey)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		out <- llm.Event{Type: llm.EventError, Error: fmt.Errorf("openai API %d: %s", resp.StatusCode, string(respBody))}
		return nil, nil
	}
	return resp, nil
}

// sseStreamState holds accumulated state while processing an SSE stream.
type sseStreamState struct {
	thinkingStarted bool
	textStarted     bool
	toolCallArgs    []string
	toolCallID      string
	toolCallName    string
	usage           llm.Usage
}

// processChunk handles one SSE chunk: emits thinking/text/toolcall events and updates state.
// Returns true if the stream is done (finish_reason was set).
func (s *sseStreamState) processChunk(chunk *streamChunk, out chan<- llm.Event) bool {
	choice := &chunk.Choices[0]
	delta := &choice.Delta

	if delta.ReasoningContent != "" {
		if !s.thinkingStarted {
			s.thinkingStarted = true
			out <- llm.Event{Type: llm.EventThinkingStart}
		}
		out <- llm.Event{Type: llm.EventThinkingDelta, ThinkingDelta: delta.ReasoningContent}
	}
	if delta.Content != "" {
		if s.thinkingStarted {
			out <- llm.Event{Type: llm.EventThinkingEnd}
			s.thinkingStarted = false
		}
		if !s.textStarted {
			s.textStarted = true
			out <- llm.Event{Type: llm.EventTextStart}
		}
		out <- llm.Event{Type: llm.EventTextDelta, TextDelta: delta.Content}
	}
	s.applyToolCallDeltas(chunk)
	if chunk.Usage != nil {
		s.usage.InputTokens = chunk.Usage.PromptTokens
		s.usage.OutputTokens = chunk.Usage.CompletionTokens
		s.usage.TotalTokens = chunk.Usage.PromptTokens + chunk.Usage.CompletionTokens
	}
	if choice.FinishReason != "" {
		s.emitFinish(choice.FinishReason, chunk, out)
		return true
	}
	return false
}

func (s *sseStreamState) applyToolCallDeltas(chunk *streamChunk) {
	if len(chunk.Choices) == 0 {
		return
	}
	delta := &chunk.Choices[0].Delta
	for i := range delta.ToolCalls {
		tc := &delta.ToolCalls[i]
		if tc.Index >= len(s.toolCallArgs) {
			for len(s.toolCallArgs) <= tc.Index {
				s.toolCallArgs = append(s.toolCallArgs, "")
			}
		}
		if tc.ID != "" {
			s.toolCallID = tc.ID
		}
		if tc.Function.Name != "" {
			s.toolCallName = tc.Function.Name
		}
		if tc.Function.Arguments != "" {
			s.toolCallArgs[tc.Index] += tc.Function.Arguments
		}
	}
}

// emitFinish emits events for end of stream (finish_reason set) and the final Done event.
func (s *sseStreamState) emitFinish(finishReason string, chunk *streamChunk, out chan<- llm.Event) {
	if s.thinkingStarted {
		out <- llm.Event{Type: llm.EventThinkingEnd}
	}
	if finishReason == "tool_calls" && s.toolCallName != "" {
		args := ""
		if len(s.toolCallArgs) > 0 {
			args = s.toolCallArgs[0]
		}
		tc := &llm.ToolCall{
			ID:        s.toolCallID,
			Name:      s.toolCallName,
			Arguments: llm.NormalizeToolCallArguments(json.RawMessage(args)),
		}
		out <- llm.Event{Type: llm.EventToolCallStart, ToolCall: tc}
		out <- llm.Event{Type: llm.EventToolCallEnd, ToolCall: tc}
	} else {
		if !s.textStarted {
			out <- llm.Event{Type: llm.EventTextStart}
		}
		out <- llm.Event{Type: llm.EventTextEnd}
	}
	if chunk.Usage != nil {
		s.usage.InputTokens = chunk.Usage.PromptTokens
		s.usage.OutputTokens = chunk.Usage.CompletionTokens
		s.usage.TotalTokens = chunk.Usage.PromptTokens + chunk.Usage.CompletionTokens
	}
	out <- llm.Event{Type: llm.EventDone, StopReason: finishReason, Usage: &s.usage}
}

// emitEnd emits ThinkingEnd/TextEnd when the stream ends without a finish_reason (e.g. [DONE]).
func (s *sseStreamState) emitEnd(out chan<- llm.Event) {
	if s.thinkingStarted {
		out <- llm.Event{Type: llm.EventThinkingEnd}
	}
	if s.textStarted {
		out <- llm.Event{Type: llm.EventTextEnd}
	} else if !s.thinkingStarted && s.toolCallID == "" {
		out <- llm.Event{Type: llm.EventTextStart}
		out <- llm.Event{Type: llm.EventTextEnd}
	}
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
		// DeepSeek thinking mode: https://api-docs.deepseek.com/zh-cn/guides/thinking_mode
		if model.Reasoning != keys.ThinkingNone || opts.ReasoningEffort != keys.ThinkingNone {
			reqBody.Thinking = &deepSeekThinking{Type: "enabled"}
		}
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
	req.Header.Set(keys.ContentType, "application/json")
	req.Header.Set(keys.Authorization, "Bearer "+c.apiKey)

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
		Thinking: choice.Message.ReasoningContent,
	}
	// If the model responded with tool calls, map the first one to llm.ToolCall.
	if len(choice.Message.ToolCalls) > 0 {
		tc := choice.Message.ToolCalls[0]
		msg.ToolCall = &llm.ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: llm.NormalizeToolCallArguments(tc.Function.Arguments),
		}
	} else {
		msg.Content = []llm.ContentBlock{
			{Type: "text", Text: choice.Message.Content},
		}
	}
	usage := llm.Usage{
		InputTokens:  raw.Usage.PromptTokens,
		OutputTokens: raw.Usage.CompletionTokens,
		TotalTokens:  raw.Usage.PromptTokens + raw.Usage.CompletionTokens,
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
		if role == "assistant" {
			cm.ReasoningContent = m.Thinking
			if m.ToolCall != nil {
				cm.ToolCalls = []openAIToolCall{{
					ID:       m.ToolCall.ID,
					Type:     "function",
					Function: openAIToolCallFn{Name: m.ToolCall.Name, Arguments: argumentsAsString(m.ToolCall.Arguments)},
				}}
			}
		}
		out = append(out, cm)
	}
	return out
}
