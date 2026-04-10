package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/vince-0202/acgo/pkg/communi"
	"github.com/vince-0202/acgo/pkg/config"
	"github.com/vince-0202/acgo/pkg/keys"
	"github.com/vince-0202/acgo/pkg/llm"
	"github.com/vince-0202/acgo/pkg/log"

	oai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/respjson"
	"github.com/openai/openai-go/v3/shared"
)

// Client is an OpenAI API provider implementation based on the official
// `github.com/openai/openai-go` SDK.
type Client struct {
	Settings *config.ProviderSetting
	provider string // e.g. "openai"
	baseURL  string
	apiKey   string
	models   []llm.Model

	oai oai.Client
}

// ClientOption configures a Client.
type ClientOption func(*Client)

// NewClient creates a new OpenAI SDK backed provider.
// baseURL should be the API root (e.g. https://api.openai.com/v1).
func NewClient(setting *config.ProviderSetting, opts ...ClientOption) llm.Provider {
	c := &Client{
		Settings: setting,
		provider: string(setting.Provider),
		baseURL:  setting.BaseURL,
		apiKey:   setting.ApiKey,
		models:   make([]llm.Model, 0, len(setting.Models)),
	}

	for _, o := range opts {
		o(c)
	}

	for _, model := range setting.Models {
		c.models = append(c.models, llm.Model{
			ModelSetting: model,
			Provider:     c.provider,
		})
	}

	c.oai = oai.NewClient(
		option.WithBaseURL(c.baseURL),
		option.WithAPIKey(c.apiKey),
	)
	return c
}

func (c *Client) Name() string { return c.provider }

func (c *Client) Models() []llm.Model { return c.models }

const maxOpenAIStreamAttempts = 3

// isRetryableOpenAIStreamErr detects transient SSE/JSON parse failures from OpenAI-compatible
// providers (e.g. DeepSeek) where a chunk is truncated or the connection drops mid-line.
func isRetryableOpenAIStreamErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var syn *json.SyntaxError
	if errors.As(err, &syn) {
		return true
	}
	msg := err.Error()
	if strings.Contains(msg, "unexpected end of JSON input") {
		return true
	}
	if strings.Contains(msg, "invalid character") && strings.Contains(strings.ToLower(msg), "json") {
		return true
	}
	return false
}

func (c *Client) Stream(ctx context.Context, model llm.Model, message []communi.Message, opts *llm.Options) (<-chan communi.LLMEvent, error) {
	out := make(chan communi.LLMEvent)
	go func() {
		defer close(out)

		if c.apiKey == "" {
			out <- communi.LLMEvent{Type: communi.EventError, Error: fmt.Errorf("provider api key is not set")}
			return
		}

		params := c.buildChatCompletionNewParams(message, model, opts, true)
		// Suppress full request payload logs to avoid noisy output.

		var (
			st         *streamState
			sawFinish  bool
			stopReason string
		)

		for attempt := 0; attempt < maxOpenAIStreamAttempts; attempt++ {
			if attempt > 0 {
				backoff := time.Duration(200*attempt) * time.Millisecond
				select {
				case <-ctx.Done():
					out <- communi.LLMEvent{Type: communi.EventError, Error: ctx.Err()}
					return
				case <-time.After(backoff):
				}
				log.Debugf("[llm][provider=%s][mode=stream] retrying stream (attempt %d/%d)", c.provider, attempt+1, maxOpenAIStreamAttempts)
			}

			stream := c.oai.Chat.Completions.NewStreaming(ctx, params)
			if err := stream.Err(); err != nil {
				if isRetryableOpenAIStreamErr(err) && attempt+1 < maxOpenAIStreamAttempts {
					log.Debugf("[llm][provider=%s][mode=stream] response=%s", c.provider, marshalJSON(map[string]any{
						"error": err.Error(), "retry": true,
					}))
					_ = stream.Close()
					continue
				}
				log.Debugf("[llm][provider=%s][mode=stream] response=%s", c.provider, marshalJSON(map[string]any{
					"error": err.Error(),
				}))
				out <- communi.LLMEvent{Type: communi.EventError, Error: err}
				return
			}

			out <- communi.LLMEvent{Type: communi.EventStart}

			st = &streamState{}
			sawFinish = false
			stopReason = ""
			hadChunk := false

			for stream.Next() {
				hadChunk = true
				chunk := stream.Current()
				//record usage
				st.applyUsage(chunk)
				if len(chunk.Choices) == 0 {
					continue
				}
				choice := chunk.Choices[0]
				if st.processDelta(choice.Delta, out) {
					// processDelta currently only updates state and emits incremental events.
					// Done events are emitted after the stream ends, so that usage deltas
					// are applied first when present.
				}

				if choice.FinishReason != "" {
					// Preserve the first non-empty finish_reason (should match the whole request).
					if !sawFinish {
						sawFinish = true
						stopReason = choice.FinishReason
					}
				}
			}

			streamErr := stream.Err()
			_ = stream.Close()

			if streamErr != nil {
				if isRetryableOpenAIStreamErr(streamErr) && !hadChunk && attempt+1 < maxOpenAIStreamAttempts {
					log.Debugf("[llm][provider=%s][mode=stream] response=%s", c.provider, marshalJSON(map[string]any{
						"error": streamErr.Error(), "retry": true,
					}))
					continue
				}
				log.Debugf("[llm][provider=%s][mode=stream] response=%s", c.provider, marshalJSON(map[string]any{
					"error": streamErr.Error(),
				}))
				out <- communi.LLMEvent{Type: communi.EventError, Error: streamErr}
				return
			}

			break
		}

		if sawFinish {
			st.emitFinishEvents(stopReason, out)
		} else {
			// Stream ended without an explicit finish_reason (e.g. user cancelled).
			st.emitEndEvents(out)
			stopReason = "stop"
		}
		log.Debugf("[llm][provider=%s][mode=stream] response=%s", c.provider, marshalJSON(map[string]any{
			"stopReason": stopReason,
			"usage":      st.usage,
			"toolCall": map[string]any{
				"id":   st.toolCallID,
				"name": st.toolCallName,
				"args": st.toolCallArgs,
			},
		}))
		out <- communi.LLMEvent{Type: communi.EventDone, StopReason: stopReason, Usage: &st.usage}
	}()

	return out, nil
}

func (c *Client) Complete(ctx context.Context, model llm.Model, message []communi.Message, opts *llm.Options) (communi.Message, llm.Usage, error) {
	if c.apiKey == "" {
		return communi.Message{}, llm.Usage{}, fmt.Errorf("%s is not set", c.apiKey)
	}

	params := c.buildChatCompletionNewParams(message, model, opts, false)
	// Suppress full request payload logs to avoid noisy output.

	resp, err := c.oai.Chat.Completions.New(ctx, params)
	if err != nil {
		log.Debugf("[llm][provider=%s][mode=complete] response=%s", c.provider, marshalJSON(map[string]any{
			"error": err.Error(),
		}))
		return communi.Message{}, llm.Usage{}, err
	}
	if resp == nil || len(resp.Choices) == 0 {
		return communi.Message{}, llm.Usage{}, fmt.Errorf("no choices returned from API")
	}

	choice := resp.Choices[0]
	log.Debugf("[llm][provider=%s][mode=complete] response=%s", c.provider, marshalJSON(map[string]any{
		"id":           resp.ID,
		"model":        resp.Model,
		"finishReason": choice.FinishReason,
		"message":      choice.Message,
		"usage":        resp.Usage,
	}))

	msg := communi.NewEmptyAssistantMessage()
	msg.AppendMetadata("provider", c.Name)
	// Tool calls (prefer tool_calls).
	if len(choice.Message.ToolCalls) > 0 {
		toolCallUnion := choice.Message.ToolCalls[0]
		switch toolCallUnion.Type {
		case "function":
			fn := toolCallUnion.AsFunction()
			msg.AppendToolCall(fn)
		}
	} else {
		msg.AppendTextContent(choice.Message.Content)
	}

	var usage llm.Usage
	usage.InputTokens = int(resp.Usage.PromptTokens)
	usage.OutputTokens = int(resp.Usage.CompletionTokens)
	usage.TotalTokens = int(resp.Usage.TotalTokens)

	// StopReason is surfaced by callers via streaming events; for non-streaming,
	// this provider only returns message + usage.
	_ = choice.FinishReason
	return msg, usage, nil
}

func marshalJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return `{"marshal_error":"` + err.Error() + `"}`
	}
	return string(b)
}

func (c *Client) buildChatCompletionNewParams(message []communi.Message, model llm.Model, opts *llm.Options, streaming bool) oai.ChatCompletionNewParams {
	params := oai.ChatCompletionNewParams{
		Model:    model.ID,
		Messages: convertMessages(message),
	}

	if opts != nil {
		params.Temperature = oai.Float(float64(opts.Temperature))
		if opts.MaxOutputTokens > 0 {
			params.MaxCompletionTokens = oai.Int(int64(opts.MaxOutputTokens))
		}
		if len(opts.StopSequences) == 1 {
			params.Stop = oai.ChatCompletionNewParamsStopUnion{OfString: oai.String(opts.StopSequences[0])}
		} else if len(opts.StopSequences) > 1 {
			params.Stop = oai.ChatCompletionNewParamsStopUnion{OfStringArray: opts.StopSequences}
		}

		if len(opts.Tools) > 0 {
			params.Tools = convertTools(opts.Tools)
			if opts.ToolChoice != "" {
				params.ToolChoice = mapToolChoice(opts.ToolChoice)
			}
		}

		params.Metadata = mapMetadata(opts.Metadata)
	}

	// Reasoning effort (OpenAI reasoning models).
	// For non-reasoning models this may be ignored by the backend.
	if opts != nil || model.Reasoning != keys.ThinkingNone {
		level := model.Reasoning
		if opts != nil && opts.ReasoningEffort != "" && opts.ReasoningEffort != keys.ThinkingNone {
			level = opts.ReasoningEffort
		}
		params.ReasoningEffort = keys.MapReasoningEffort(level)
		if level != keys.ThinkingNone {
			params.SetExtraFields(map[string]any{
				"thinking": map[string]any{
					"type": "enabled",
				},
			})
		}
	}

	if streaming {
		// Best effort: include usage if supported.
		params.StreamOptions = oai.ChatCompletionStreamOptionsParam{
			IncludeUsage: oai.Bool(true),
		}
	}

	return params
}

func mapToolChoice(choice string) oai.ChatCompletionToolChoiceOptionUnionParam {
	// Matches OpenAI tool_choice values.
	switch strings.ToLower(choice) {
	case "none", "auto", "required":
		return oai.ChatCompletionToolChoiceOptionUnionParam{OfAuto: oai.String(choice)}
	default:
		// Unknown hint; omit.
		return oai.ChatCompletionToolChoiceOptionUnionParam{}
	}
}

func mapMetadata(in map[string]any) shared.Metadata {
	if len(in) == 0 {
		return nil
	}
	out := make(shared.Metadata, len(in))
	for k, v := range in {
		out[k] = fmt.Sprint(v)
	}
	return out
}

func convertTools(tools []communi.ToolSchema) []oai.ChatCompletionToolUnionParam {
	out := make([]oai.ChatCompletionToolUnionParam, 0, len(tools))
	for _, t := range tools {
		fn := shared.FunctionDefinitionParam{
			Name:        t.Name,
			Description: oai.String(t.Description),
			Parameters:  shared.FunctionParameters(t.JSONSchema),
		}
		out = append(out, oai.ChatCompletionFunctionTool(fn))
	}
	return out
}

func assistantToolCallsParam(m communi.Message) []oai.ChatCompletionMessageToolCallUnionParam {
	if len(m.ToolCalls) > 0 {
		out := make([]oai.ChatCompletionMessageToolCallUnionParam, 0, len(m.ToolCalls))
		for _, tc := range m.ToolCalls {
			out = append(out, oai.ChatCompletionMessageToolCallUnionParam{
				OfFunction: &oai.ChatCompletionMessageFunctionToolCallParam{
					ID: tc.ID,
					Function: oai.ChatCompletionMessageFunctionToolCallFunctionParam{
						Name:      tc.Name,
						Arguments: string(tc.Arguments),
					},
				},
			})
		}
		return out
	}
	if m.ToolCall != nil {
		return []oai.ChatCompletionMessageToolCallUnionParam{
			{
				OfFunction: &oai.ChatCompletionMessageFunctionToolCallParam{
					ID: m.ToolCall.ID,
					Function: oai.ChatCompletionMessageFunctionToolCallFunctionParam{
						Name:      m.ToolCall.Name,
						Arguments: string(m.ToolCall.Arguments),
					},
				},
			},
		}
	}
	return nil
}

func convertMessages(msgs []communi.Message) []oai.ChatCompletionMessageParamUnion {
	out := make([]oai.ChatCompletionMessageParamUnion, 0, len(msgs))
	for _, m := range msgs {
		contentText := m.ContentBlocksToText()
		switch m.Role {
		case keys.AgentRoleSystem:
			out = append(out, oai.SystemMessage(contentText))
		case keys.AgentRoleUser:
			out = append(out, oai.UserMessage(contentText))
		case keys.AgentRoleTool:
			out = append(out, oai.ToolMessage(contentText, m.ToolCallId()))
		case keys.AgentRoleAssistant:
			assistantMsg := oai.AssistantMessage(contentText)
			assistantMsg.OfAssistant.SetExtraFields(map[string]any{
				"reasoning_content": m.Thinking,
			})
			if calls := assistantToolCallsParam(m); len(calls) > 0 {
				assistantMsg.OfAssistant.ToolCalls = calls
			}
			out = append(out, assistantMsg)
		default:
			// Drop unknown roles.
		}
	}
	return out
}

type streamState struct {
	thinkingStarted bool
	textStarted     bool

	toolCallArgs []string
	toolCallID   string
	toolCallName string

	usage communi.Usage
}

func (s *streamState) applyUsage(chunk oai.ChatCompletionChunk) {
	if chunk.JSON.Usage.Valid() {
		// CompletionUsage is int64 based. Safe to cast to int.
		s.usage.InputTokens = int(chunk.Usage.PromptTokens)
		s.usage.OutputTokens = int(chunk.Usage.CompletionTokens)
		s.usage.TotalTokens = int(chunk.Usage.TotalTokens)
	}

}

func (s *streamState) processDelta(delta oai.ChatCompletionChunkChoiceDelta, out chan<- communi.LLMEvent) bool {
	// Thinking: in some OpenAI-compatible servers (e.g. DeepSeek), a custom
	// `reasoning_content` field may appear in the delta JSON.
	reasoningDelta := extraString(delta.JSON.ExtraFields, "reasoning_content")
	// Fallback: if the SDK didn't preserve the field into ExtraFields, parse it
	// from the raw delta JSON.
	if reasoningDelta == "" {
		reasoningDelta = reasoningContentFromRaw(delta.RawJSON())
	}
	if reasoningDelta != "" {
		if !s.thinkingStarted {
			s.thinkingStarted = true
			out <- communi.LLMEvent{Type: communi.EventThinkingStart}
		}
		out <- communi.LLMEvent{Type: communi.EventThinkingDelta, ThinkingDelta: reasoningDelta}
	}

	// Text.
	if delta.Content != "" {
		if s.thinkingStarted {
			out <- communi.LLMEvent{Type: communi.EventThinkingEnd}
			s.thinkingStarted = false
		}
		if !s.textStarted {
			s.textStarted = true
			out <- communi.LLMEvent{Type: communi.EventTextStart}
		}
		out <- communi.LLMEvent{Type: communi.EventTextDelta, TextDelta: delta.Content}
	}

	// Tool calls: only accumulate arguments; toolcall start/end is emitted on finish_reason.
	for _, tc := range delta.ToolCalls {
		idx := int(tc.Index)
		for len(s.toolCallArgs) <= idx {
			s.toolCallArgs = append(s.toolCallArgs, "")
		}
		if tc.ID != "" {
			s.toolCallID = tc.ID
		}
		if tc.Function.Name != "" {
			s.toolCallName = tc.Function.Name
		}
		if tc.Function.Arguments != "" {
			s.toolCallArgs[idx] += tc.Function.Arguments
		}
	}
	return true
}

func (s *streamState) emitFinishEvents(finishReason string, out chan<- communi.LLMEvent) {
	// Close thinking stream.
	if s.thinkingStarted {
		out <- communi.LLMEvent{Type: communi.EventThinkingEnd}
	}

	// Prefer toolcall when finish_reason indicates tool use.
	if finishReason == "tool_calls" && s.toolCallName != "" {
		args := ""
		if len(s.toolCallArgs) > 0 {
			args = s.toolCallArgs[0]
		}
		tc := &communi.ToolCallRequest{
			ID:        s.toolCallID,
			Name:      s.toolCallName,
			Arguments: llm.NormalizeToolCallArguments(json.RawMessage(args)),
		}
		out <- communi.LLMEvent{Type: communi.EventToolCallStart, ToolCall: tc}
		out <- communi.LLMEvent{Type: communi.EventToolCallEnd, ToolCall: tc}
		return
	}

	// Otherwise treat as plain text.
	if !s.textStarted {
		out <- communi.LLMEvent{Type: communi.EventTextStart}
	}
	out <- communi.LLMEvent{Type: communi.EventTextEnd}
}

func (s *streamState) emitEndEvents(out chan<- communi.LLMEvent) {
	if s.thinkingStarted {
		out <- communi.LLMEvent{Type: communi.EventThinkingEnd}
	}
	if s.textStarted {
		out <- communi.LLMEvent{Type: communi.EventTextEnd}
	} else if !s.thinkingStarted && s.toolCallID == "" {
		out <- communi.LLMEvent{Type: communi.EventTextStart}
		out <- communi.LLMEvent{Type: communi.EventTextEnd}
	}
}

func extraString(fields map[string]respjson.Field, key string) string {
	if fields == nil {
		return ""
	}
	f, ok := fields[key]
	if !ok || !f.Valid() {
		return ""
	}
	raw := f.Raw()
	if raw == "" || raw == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return raw
	}
	return s
}

func reasoningContentFromRaw(raw string) string {
	if raw == "" {
		return ""
	}
	var obj struct {
		ReasoningContent json.RawMessage `json:"reasoning_content"`
	}
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return ""
	}
	if len(obj.ReasoningContent) == 0 || string(obj.ReasoningContent) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(obj.ReasoningContent, &s); err == nil {
		return s
	}
	// If it's not a JSON string, return its raw JSON.
	return string(obj.ReasoningContent)
}
