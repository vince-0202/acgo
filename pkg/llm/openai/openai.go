package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"go-pi/pkg/llm"
)

// Client is a minimal OpenAI client implementing the llm.Provider interface.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new OpenAI client with a default HTTP client.
func NewClient(baseURL string) *Client {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

func (c *Client) Name() string {
	return "openai"
}

// Models returns a small set of commonly used OpenAI models.
func (c *Client) Models() []llm.Model {
	return []llm.Model{
		{
			ID:            "gpt-4o-mini",
			Name:          "GPT-4o Mini",
			API:           "chat-completions",
			Provider:      c.Name(),
			ContextWindow: 128_000,
			MaxTokens:     16_000,
			Input:         []llm.InputCapability{llm.InputCapabilityText},
			Reasoning:     llm.ReasoningLow,
		},
	}
}

// chatCompletionRequest mirrors the OpenAI Chat Completions API shape (simplified).
type chatCompletionRequest struct {
	Model    string                 `json:"model"`
	Messages []chatMessage          `json:"messages"`
	Stream   bool                   `json:"stream"`
	MaxTokens int                   `json:"max_tokens,omitempty"`
	Temperature float32             `json:"temperature,omitempty"`
	Stop     []string               `json:"stop,omitempty"`
	Metadata map[string]any         `json:"metadata,omitempty"`
}

type chatMessage struct {
	Role    string        `json:"role"`
	Content []contentPart `json:"content"`
}

type contentPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type chatCompletionResponse struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
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
		out <- llm.Event{Type: llm.EventTextStart}
		// For now, emit the full content as a single delta.
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
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return llm.Message{}, llm.Usage{}, fmt.Errorf("OPENAI_API_KEY is not set")
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
		reqBody.Metadata = opts.Metadata
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return llm.Message{}, llm.Usage{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return llm.Message{}, llm.Usage{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return llm.Message{}, llm.Usage{}, err
	}
	defer resp.Body.Close()

	var raw struct {
		Choices []struct {
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return llm.Message{}, llm.Usage{}, err
	}
	if len(raw.Choices) == 0 {
		return llm.Message{}, llm.Usage{}, fmt.Errorf("no choices returned from OpenAI")
	}

	choice := raw.Choices[0]
	msg := llm.Message{
		Role:     llm.RoleAssistant,
		Provider: c.Name(),
		Content: []llm.ContentBlock{
			{Type: "text", Text: choice.Message.Content},
		},
	}
	usage := llm.Usage{
		InputTokens:  raw.Usage.PromptTokens,
		OutputTokens: raw.Usage.CompletionTokens,
	}
	return msg, usage, nil
}

func convertMessages(msgs []llm.Message) []chatMessage {
	out := make([]chatMessage, 0, len(msgs))
	for _, m := range msgs {
		role := string(m.Role)
		parts := make([]contentPart, 0, len(m.Content))
		for _, c := range m.Content {
			if c.Type == "text" {
				parts = append(parts, contentPart{Type: "text", Text: c.Text})
			}
		}
		out = append(out, chatMessage{
			Role:    role,
			Content: parts,
		})
	}
	return out
}

// Register registers the OpenAI provider and its models into the global registries.
func Register() {
	client := NewClient("")
	llm.RegisterProvider(client)
	for _, m := range client.Models() {
		llm.RegisterModel(m)
	}
}

