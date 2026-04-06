package communi

import (
	"encoding/json"
	"github.com/vince-0202/acgo/pkg/errors"
)

// AgentEventType mirrors the high-level event types from pi-agent-core.
type AgentEventType string

const (
	EventAgentStart          AgentEventType = "agent_start"
	EventAgentEnd            AgentEventType = "agent_end"
	EventTurnStart           AgentEventType = "turn_start"
	EventTurnEnd             AgentEventType = "turn_end"
	EventMessageStart        AgentEventType = "message_start"
	EventMessageUpdate       AgentEventType = "message_update"
	EventMessageEnd          AgentEventType = "message_end"
	EventToolExecutionStart  AgentEventType = "tool_execution_start"
	EventToolExecutionUpdate AgentEventType = "tool_execution_update"
	EventToolExecutionEnd    AgentEventType = "tool_execution_end"
)

// AgentEvent carries information about state changes, messages and tool executions.
type AgentEvent struct {
	Type       AgentEventType
	AgentID    string
	TurnID     string
	Message    *Message
	ToolName   string
	ToolCallID string
	ToolArgs   json.RawMessage
	LlmEvent   *LLMEvent
	Error      error
	ErrorKind  errors.ErrKind // classification for UI; set when Error is set
}

// Listener is a callback that receives events from the Agent.
type Listener func(AgentEvent)

// LLMEventType enumerates the canonical event kinds produced by all providers.
type LLMEventType string

const (
	EventStart         LLMEventType = "start"
	EventTextStart     LLMEventType = "text_start"
	EventTextDelta     LLMEventType = "text_delta"
	EventTextEnd       LLMEventType = "text_end"
	EventThinkingStart LLMEventType = "thinking_start"
	EventThinkingDelta LLMEventType = "thinking_delta"
	EventThinkingEnd   LLMEventType = "thinking_end"
	EventToolCallStart LLMEventType = "toolcall_start"
	EventToolCallDelta LLMEventType = "toolcall_delta"
	EventToolCallEnd   LLMEventType = "toolcall_end"
	EventDone          LLMEventType = "done"
	EventError         LLMEventType = "error"
)

// LLMEvent is the unified streaming event structure.
type LLMEvent struct {
	Type          LLMEventType     // event type
	TextDelta     string           // for text_delta events
	ThinkingDelta string           // for thinking_delta events
	ToolCall      *ToolCallRequest // for toolcall_* events
	Error         error            // for error events
	StopReason    string           // for done events ("stop", "length", "toolUse", "error", "aborted", ...)
	Usage         *Usage           // optional usage info, typically set on the final done event
}

// Usage optionally tracks token usage for a call.
type Usage struct {
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
	TotalTokens  int `json:"total_tokens,omitempty"`
}
