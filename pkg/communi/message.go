package communi

import (
	"encoding/json"
	"github.com/openai/openai-go/v3"
	"github.com/vince-0202/acgo/pkg/keys"
	"strings"
	"time"
)

type ContextType string

func NewEmptyAssistantMessage() Message {
	return Message{
		Role:    keys.AgentRoleAssistant,
		Content: make([]*ContentBlock, 0),
	}
}

func NewAssistantMessage(id, context string) Message {
	return Message{
		ID:      "assistant-" + id,
		Role:    keys.AgentRoleAssistant,
		Content: []*ContentBlock{NewTextContentBlock(context)},
	}
}

func NewUserMessage(id string, content string) Message {
	return Message{
		ID:   "user-" + id,
		Role: keys.AgentRoleUser,
		Content: []*ContentBlock{
			NewTextContentBlock(content),
		},
	}
}

func NewUserMessageWithoutId(content string) Message {
	return Message{
		Role: keys.AgentRoleUser,
		Content: []*ContentBlock{
			NewTextContentBlock(content),
		},
	}
}

func NewSystemMessage(id string, content string) Message {
	return Message{
		ID:   "system-" + id,
		Role: keys.AgentRoleSystem,
		Content: []*ContentBlock{
			NewTextContentBlock(content),
		},
	}
}

func NewSystemMessageWithoutId(content string) Message {
	return Message{
		Role: keys.AgentRoleSystem,
		Content: []*ContentBlock{
			NewTextContentBlock(content),
		},
	}
}

// Message is the application-facing message type.
type Message struct {
	ID         string                // unique identifier within a session
	Metadata   map[string]any        // arbitrary metadata
	Role       keys.AgentMessageRole `json:"role"`               // logical role
	Content    []*ContentBlock       `json:"content,omitempty"`  // rendered text content (for UI)
	Thinking   string                `json:"thinking,omitempty"` // reasoning/thinking stream from models that support it (e.g. DeepSeek R1, o1)
	ToolCall   *ToolCallRequest      `json:"tool_call,omitempty"`
	ToolResult *ToolCallResult       `json:"tool_result,omitempty"`
	CreatedAt  time.Time             `json:"created,omitempty"`
	IsError    bool                  // whether this message represents an error
}

func (m *Message) AppendTextContent(text string) {
	m.Content = append(m.Content, &ContentBlock{
		Text: text,
		Type: "text",
	})
}

func (m *Message) AppendMetadata(k string, v any) {
	m.Metadata[k] = v
}

func (m *Message) AppendToolCall(fn openai.ChatCompletionMessageFunctionToolCall) {
	m.ToolCall = &ToolCallRequest{
		ID:        fn.ID,
		Name:      fn.Function.Name,
		Arguments: json.RawMessage(fn.Function.Arguments),
	}
}

func (m *Message) ContentBlocksToText() string {
	var b strings.Builder
	for _, c := range m.Content {
		if c.Type == "text" {
			b.WriteString(c.Text)
		}
	}
	return b.String()
}

func (m *Message) ToolCallId() string {
	return m.ToolCall.ID
}

func (m *Message) AppendTextValue(delta string) {
	m.Content[len(m.Content)-1].AppendText(delta)
}

// ContentBlock represents a piece of content within a message.
// For now we support text and image; additional types (e.g. code) can be added later.
type ContentBlock struct {
	Type        string `json:"type"`                   // "text" | "image" | ...
	Text        string `json:"text,omitempty"`         // for text content
	ImageBase64 string `json:"image_base64,omitempty"` // base64 encoded image data
	MimeType    string `json:"mime_type,omitempty"`    // image mime-type, e.g. "image/png"
}

func (cb *ContentBlock) AppendText(text string) {
	cb.Text += text
}

func NewTextContentBlock(text string) *ContentBlock {
	return &ContentBlock{
		Type: "text",
		Text: text,
	}
}
