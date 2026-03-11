package agent

import "go-pi/pkg/llm"

// EventType mirrors the high-level event types from pi-agent-core.
type EventType string

const (
	EventAgentStart        EventType = "agent_start"
	EventAgentEnd          EventType = "agent_end"
	EventTurnStart         EventType = "turn_start"
	EventTurnEnd           EventType = "turn_end"
	EventMessageStart      EventType = "message_start"
	EventMessageUpdate     EventType = "message_update"
	EventMessageEnd        EventType = "message_end"
	EventToolExecutionStart EventType = "tool_execution_start"
	EventToolExecutionUpdate EventType = "tool_execution_update"
	EventToolExecutionEnd  EventType = "tool_execution_end"
)

// Event carries information about state changes, messages and tool executions.
type Event struct {
	Type      EventType
	AgentID   string
	TurnID    string
	Message   *AgentMessage
	ToolName  string
	LlmEvent  *llm.Event
	Error     error
}

// Listener is a callback that receives events from the Agent.
type Listener func(Event)

