package agent

import (
	"context"
	"sync"
	"time"

	"go-pi/pkg/llm"
)

// Options configures an Agent instance.
type Options struct {
	InitialState   AgentState
	StreamFn       llm.StreamFunc
	ConvertToLlm   func([]AgentMessage) []llm.Message
	TransformContext func([]AgentMessage, context.Context) []AgentMessage
}

// Agent coordinates LLM calls, tools and state updates.
type Agent struct {
	id       string
	state    AgentState
	streamFn llm.StreamFunc

	convertToLlm     func([]AgentMessage) []llm.Message
	transformContext func([]AgentMessage, context.Context) []AgentMessage

	listenersMu sync.RWMutex
	listeners   []Listener

	currentCancel context.CancelFunc
}

// New creates a new Agent with the given options.
func New(id string, opts Options) *Agent {
	a := &Agent{
		id:    id,
		state: opts.InitialState,
	}
	if opts.StreamFn != nil {
		a.streamFn = opts.StreamFn
	}
	if a.streamFn == nil {
		// default streamFn dispatches to the provider registered for the model.
		a.streamFn = defaultStreamFn
	}
	if opts.ConvertToLlm != nil {
		a.convertToLlm = opts.ConvertToLlm
	} else {
		a.convertToLlm = defaultConvertToLlm
	}
	if opts.TransformContext != nil {
		a.transformContext = opts.TransformContext
	} else {
		a.transformContext = defaultTransformContext
	}
	return a
}

// State returns a copy of the current state.
func (a *Agent) State() AgentState {
	return a.state
}

// Subscribe registers a listener for events. It returns an unsubscribe function.
func (a *Agent) Subscribe(l Listener) func() {
	a.listenersMu.Lock()
	defer a.listenersMu.Unlock()
	a.listeners = append(a.listeners, l)
	return func() {
		a.listenersMu.Lock()
		defer a.listenersMu.Unlock()
		// Functional comparison is not supported; remove by index.
		for i := range a.listeners {
			if &a.listeners[i] == &l {
				a.listeners = append(a.listeners[:i], a.listeners[i+1:]...)
				return
			}
		}
	}
}

func (a *Agent) emit(e Event) {
	a.listenersMu.RLock()
	defer a.listenersMu.RUnlock()
	for _, l := range a.listeners {
		l(e)
	}
}

// SetSystemPrompt updates the system prompt.
func (a *Agent) SetSystemPrompt(prompt string) {
	a.state.SystemPrompt = prompt
}

// SetModel updates the model.
func (a *Agent) SetModel(m llm.Model) {
	a.state.Model = m
}

// SetThinkingLevel updates the thinking level.
func (a *Agent) SetThinkingLevel(level ThinkingLevel) {
	a.state.ThinkingLevel = level
}

// SetTools replaces the tool set.
func (a *Agent) SetTools(tools []AgentTool) {
	a.state.Tools = tools
}

// AppendMessage appends a message to the history.
func (a *Agent) AppendMessage(msg AgentMessage) {
	a.state.Messages = append(a.state.Messages, msg)
}

// ClearMessages clears all messages.
func (a *Agent) ClearMessages() {
	a.state.Messages = nil
}

// Reset clears messages and error state.
func (a *Agent) Reset() {
	a.state.Messages = nil
	a.state.Error = nil
	a.state.StreamMessage = nil
	a.state.PendingToolCalls = nil
}

// Prompt sends a new user message and runs a single LLM turn (no tool calls yet).
func (a *Agent) Prompt(ctx context.Context, content string) error {
	turnID := time.Now().UTC().Format(time.RFC3339Nano)

	userMsg := AgentMessage{
		ID:      "user-" + turnID,
		Role:    RoleUser,
		Content: content,
	}
	a.AppendMessage(userMsg)

	a.emit(Event{Type: EventAgentStart, AgentID: a.id})
	a.emit(Event{Type: EventTurnStart, AgentID: a.id, TurnID: turnID})
	a.emit(Event{Type: EventMessageStart, AgentID: a.id, TurnID: turnID, Message: &userMsg})
	a.emit(Event{Type: EventMessageEnd, AgentID: a.id, TurnID: turnID, Message: &userMsg})

	ctx, cancel := context.WithCancel(ctx)
	a.currentCancel = cancel
	a.state.IsStreaming = true
	defer func() {
		a.state.IsStreaming = false
		a.currentCancel = nil
	}()

	// Prepare LLM context.
	messages := a.transformContext(a.state.Messages, ctx)
	llmMessages := a.convertToLlm(messages)
	llmCtx := llm.Context{Messages: llmMessages}

	events, err := a.streamFn(ctx, a.state.Model, llmCtx, &llm.Options{})
	if err != nil {
		a.state.Error = err
		a.emit(Event{Type: EventTurnEnd, AgentID: a.id, TurnID: turnID, Error: err})
		a.emit(Event{Type: EventAgentEnd, AgentID: a.id, Error: err})
		return err
	}

	assistant := AgentMessage{
		ID:      "assistant-" + turnID,
		Role:    RoleAssistant,
		Content: "",
	}
	a.state.StreamMessage = &assistant
	a.emit(Event{Type: EventMessageStart, AgentID: a.id, TurnID: turnID, Message: &assistant})

	for ev := range events {
		switch ev.Type {
		case llm.EventTextDelta:
			assistant.Content += ev.TextDelta
			a.emit(Event{
				Type:     EventMessageUpdate,
				AgentID:  a.id,
				TurnID:   turnID,
				Message:  &assistant,
				LlmEvent: &ev,
			})
		case llm.EventError:
			a.state.Error = ev.Error
			assistant.IsError = true
		case llm.EventDone:
			// no-op, handled after loop
		}
	}

	// Finalize assistant message.
	a.state.StreamMessage = nil
	a.AppendMessage(assistant)
	a.emit(Event{Type: EventMessageEnd, AgentID: a.id, TurnID: turnID, Message: &assistant})
	a.emit(Event{Type: EventTurnEnd, AgentID: a.id, TurnID: turnID})
	a.emit(Event{Type: EventAgentEnd, AgentID: a.id})

	return a.state.Error
}

// Abort cancels the current LLM call if one is active.
func (a *Agent) Abort() {
	if a.currentCancel != nil {
		a.currentCancel()
	}
}

// defaultConvertToLlm converts AgentMessage to llm.Message by mapping roles and content.
func defaultConvertToLlm(msgs []AgentMessage) []llm.Message {
	out := make([]llm.Message, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case RoleSystem, RoleUser, RoleAssistant, RoleTool:
			role := llm.Role(string(m.Role))
			out = append(out, llm.Message{
				Role: role,
				Content: []llm.ContentBlock{
					{Type: "text", Text: m.Content},
				},
			})
		default:
			// Drop notification or other UI-only messages from LLM context.
		}
	}
	return out
}

// defaultTransformContext is a placeholder that simply returns the original messages.
func defaultTransformContext(msgs []AgentMessage, _ context.Context) []AgentMessage {
	return msgs
}

// defaultStreamFn looks up the provider for the given model and calls its Stream function.
func defaultStreamFn(ctx context.Context, model llm.Model, context llm.Context, opts *llm.Options) (<-chan llm.Event, error) {
	provider, ok := llm.GetProvider(model.Provider)
	if !ok {
		ch := make(chan llm.Event, 1)
		ch <- llm.Event{
			Type:  llm.EventError,
			Error: llm.ErrUnknownProvider(model.Provider),
		}
		close(ch)
		return ch, nil
	}
	return provider.Stream(ctx, model, context, opts)
}

