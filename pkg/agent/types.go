package agent

import (
	"go-pi/pkg/llm"
)

// AgentMessageRole describes high-level roles, including custom ones like notification.
type AgentMessageRole string

const (
	RoleSystem       AgentMessageRole = "system"
	RoleUser         AgentMessageRole = "user"
	RoleAssistant    AgentMessageRole = "assistant"
	RoleTool         AgentMessageRole = "tool"
	RoleNotification AgentMessageRole = "notification"
)

// AgentMessage is the application-facing message type.
type AgentMessage struct {
	ID         string                 // unique identifier within a session
	Role       AgentMessageRole       // logical role
	Content    string                 // rendered text content (for UI)
	LlmMessage *llm.Message           // backing LLM message when applicable
	IsError    bool                   // whether this message represents an error
	Metadata   map[string]any         // arbitrary metadata
}

// ThinkingLevel controls reasoning intensity, similar to pi-agent-core.
type ThinkingLevel string

const (
	ThinkingMinimal ThinkingLevel = "minimal"
	ThinkingLow     ThinkingLevel = "low"
	ThinkingMedium  ThinkingLevel = "medium"
	ThinkingHigh    ThinkingLevel = "high"
	ThinkingXHigh   ThinkingLevel = "xhigh"
)

// AgentState holds the mutable state of an Agent instance.
type AgentState struct {
	SystemPrompt  string
	Model         llm.Model
	ThinkingLevel ThinkingLevel
	Tools         []AgentTool
	Messages      []AgentMessage

	IsStreaming   bool
	StreamMessage *AgentMessage
	PendingToolCalls []llm.ToolCall
	Error         error
}

