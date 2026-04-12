package agent_new

import (
	"github.com/vince-0202/acgo/pkg/communi"
	"github.com/vince-0202/acgo/pkg/errors"
)

// AgentEventType mirrors the high-level event types from pi-agent-core.
type EventType string

const (
	EventAgentStart          EventType = "agent_start"
	EventAgentEnd            EventType = "agent_end"
	EventTurnStart           EventType = "turn_start"
	EventTurnEnd             EventType = "turn_end"
	EventBeforeLLMCall       EventType = "before_llm_call"
	EventAfterLLMCall        EventType = "after_llm_call"
	EventMessageStart        EventType = "message_start"
	EventMessageUpdate       EventType = "message_update"
	EventMessageEnd          EventType = "message_end"
	EventBeforeToolExecution EventType = "before_tool_execution"
	EventToolExecutionStart  EventType = "tool_execution_start"
	EventToolExecutionUpdate EventType = "tool_execution_update"
	EventToolExecutionEnd    EventType = "tool_execution_end"
	EventAfterToolExecution  EventType = "after_tool_execution"
)

func NewEvent(opts ...EventOption) Event {
	e := &Event{}
	for _, o := range opts {
		o(e)
	}
	return *e
}

// Event carries information about state changes, messages and tool executions.
type Event struct {
	Type       EventType
	Agent      *Agent
	TurnID     string
	Message    *communi.Message
	Tool       Tool
	ToolCallID string
	LlmEvent   *communi.LLMEvent
	Error      *errors.AgentError
}

type EventOption func(*Event)

func WithEventType(et EventType) EventOption {
	return func(e *Event) {
		e.Type = et
	}
}

func WithEventAgent(a *Agent) EventOption {
	return func(e *Event) {
		e.Agent = a
	}
}

func WithEventTooCallId(tooCallId string) EventOption {
	return func(e *Event) {
		e.ToolCallID = tooCallId
	}
}

func WithEventTool(tool Tool) EventOption {
	return func(e *Event) {
		e.Tool = tool
	}
}

func WithEventTurnId(turnID string) EventOption {
	return func(e *Event) {
		e.TurnID = turnID
	}
}

func WithEventMessage(msg *communi.Message) EventOption {
	return func(e *Event) {
		e.Message = msg
	}
}

func WithEventError(err *errors.AgentError) EventOption {
	return func(e *Event) {
		e.Error = err
	}
}

func WithEventLLMEvent(el *communi.LLMEvent) EventOption {
	return func(e *Event) {
		e.LlmEvent = el
	}
}
