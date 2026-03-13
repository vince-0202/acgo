package llm

// Role describes the logical role of a message within a conversation.
// It is intentionally close to common provider schemas (system/user/assistant/tool).
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// ContentBlock represents a piece of content within a message.
// For now we support text and image; additional types (e.g. code) can be added later.
type ContentBlock struct {
	Type        string `json:"type"`                   // "text" | "image" | ...
	Text        string `json:"text,omitempty"`         // for text content
	ImageBase64 string `json:"image_base64,omitempty"` // base64 encoded image data
	MimeType    string `json:"mime_type,omitempty"`    // image mime-type, e.g. "image/png"
}

// Message models a single message in the conversation context.
// ToolCall and ToolResult are attached for providers that support tool usage.
// For role "tool", ToolCallID must be set and Content is the tool result text.
type Message struct {
	Role       Role           `json:"role"`
	Content    []ContentBlock `json:"content,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"` // required when Role is RoleTool for OpenAI-style APIs
	Provider   string         `json:"provider,omitempty"`
	Thinking   string         `json:"thinking,omitempty"`
	ToolCall   *ToolCall      `json:"tool_call,omitempty"`
	ToolResult *ToolResult    `json:"tool_result,omitempty"`
}

// Context contains the full list of messages for a call.
// It can be freely manipulated before being passed to a provider.
type Context struct {
	Messages []Message `json:"messages"`
}
