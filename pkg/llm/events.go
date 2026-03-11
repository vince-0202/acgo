package llm

// EventType enumerates the canonical event kinds produced by all providers.
type EventType string

const (
	EventStart         EventType = "start"
	EventTextStart     EventType = "text_start"
	EventTextDelta     EventType = "text_delta"
	EventTextEnd       EventType = "text_end"
	EventThinkingStart EventType = "thinking_start"
	EventThinkingDelta EventType = "thinking_delta"
	EventThinkingEnd   EventType = "thinking_end"
	EventToolCallStart EventType = "toolcall_start"
	EventToolCallDelta EventType = "toolcall_delta"
	EventToolCallEnd   EventType = "toolcall_end"
	EventDone          EventType = "done"
	EventError         EventType = "error"
)

// Event is the unified streaming event structure.
type Event struct {
	Type          EventType // event type
	TextDelta     string    // for text_delta events
	ThinkingDelta string    // for thinking_delta events
	ToolCall      *ToolCall // for toolcall_* events
	Error         error     // for error events
	StopReason    string    // for done events ("stop", "length", "toolUse", "error", "aborted", ...)
}

// Usage optionally tracks token usage for a call.
type Usage struct {
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
}

