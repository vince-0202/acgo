package agent

import (
	"acgo/pkg/keys"
	"acgo/pkg/llm"
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

// AgentState holds the mutable state of an Agent instance.
type AgentState struct {
	SystemPrompt  string
	Model         llm.Model
	ThinkingLevel keys.ThinkingLevel
	Tools         []AgentTool
	Messages      []AgentMessage

	IsStreaming      bool
	StreamMessage    *AgentMessage
	PendingToolCalls []llm.ToolCall
	Error            error
	LastErrorKind    ErrKind // classification of Error for UI

	// LastUsage captures the most recent token usage reported by the LLM
	// provider for a completed turn, if available.
	LastUsage *llm.Usage

	// LastStopReason records the last completion stop reason reported by
	// the provider (e.g. "stop", "length", "toolUse", "error", "aborted").
	LastStopReason string

	// SteeringQueue holds user/steering messages to process next; consumed before FollowUpQueue.
	// When the agent is busy, callers may enqueue here; after the current turn ends, these are processed first.
	SteeringQueue []AgentMessage

	// FollowUpQueue holds follow-up messages; consumed after SteeringQueue is empty.
	FollowUpQueue []AgentMessage
}
