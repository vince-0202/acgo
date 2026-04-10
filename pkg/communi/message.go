package communi

import (
	"encoding/json"
	"github.com/openai/openai-go/v3"
	"github.com/vince-0202/acgo/pkg/keys"
	"strings"
	"sync"
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
	ID       string                // unique identifier within a session
	Metadata map[string]any        // arbitrary metadata
	Role     keys.AgentMessageRole `json:"role"`               // logical role
	Content  []*ContentBlock       `json:"content,omitempty"`  // rendered text content (for UI)
	Thinking string                `json:"thinking,omitempty"` // reasoning/thinking stream from models that support it (e.g. DeepSeek R1, o1)
	ToolCall *ToolCallRequest      `json:"tool_call,omitempty"`
	// ToolCalls is the full parallel tool_calls set for this assistant turn (OpenAI-style).
	// When non-empty, providers should prefer this over ToolCall for serialization.
	ToolCalls  []ToolCallRequest `json:"tool_calls,omitempty"`
	ToolResult *ToolCallResult   `json:"tool_result,omitempty"`
	CreatedAt  time.Time         `json:"created,omitempty"`
	IsError    bool              // whether this message represents an error
}

func (m *Message) AppendTextContent(text string) {
	m.Content = append(m.Content, &ContentBlock{
		Text: text,
		Type: "text",
	})
}

func (m *Message) AppendMetadata(k string, v any) {
	if m.Metadata == nil {
		m.Metadata = make(map[string]any)
	}
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

type MessageBus struct {
	mu      sync.Mutex
	inbox   map[string][]MessageEnvelope
	byID    map[string]MessageEnvelope
	history []MessageEnvelope
}

type MessageEnvelope struct {
	ID            string         `json:"id"`
	FromSubID     string         `json:"from_sub_id"`
	ToSubID       string         `json:"to_sub_id"`
	Intent        string         `json:"intent,omitempty"`
	Payload       string         `json:"payload,omitempty"`
	CorrelationID string         `json:"correlation_id,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	AckedAt       *time.Time     `json:"acked_at,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
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

// MetaSuppressTranscript marks a user message that should not appear in chat UIs
// (e.g. internal cron dispatch). Handled by TUI and similar surfaces.
const MetaSuppressTranscript = "acgo.suppress_transcript"

const (
	MetaMsgType       = "acgo.msg_type"
	MetaFromAgent     = "acgo.from"
	MetaToAgent       = "acgo.to"
	MetaIntent        = "acgo.intent"
	MetaCorrelationID = "acgo.correlation_id"
)

// SuppressTranscript reports whether this message should be hidden from transcripts.
func (m *Message) SuppressTranscript() bool {
	if m == nil || m.Metadata == nil {
		return false
	}
	v, ok := m.Metadata[MetaSuppressTranscript]
	if !ok {
		return false
	}
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return x == "1" || x == "true" || x == "yes"
	default:
		return false
	}
}
