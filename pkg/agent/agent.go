package agent

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"acgo/pkg/llm"
)

// Options configures an Agent instance.
type Options struct {
	InitialState     AgentState
	StreamFn         llm.StreamFunc
	ConvertToLlm     func([]AgentMessage) []llm.Message
	TransformContext func([]AgentMessage, context.Context) []AgentMessage
}

// Agent coordinates LLM calls, tools and state updates.
type Agent struct {
	id       string
	state    AgentState
	streamFn llm.StreamFunc

	convertToLlm     func([]AgentMessage) []llm.Message
	transformContext func([]AgentMessage, context.Context) []AgentMessage

	listenersMu    sync.RWMutex
	listeners      []listenerSlot
	nextListenerID int

	currentCancel context.CancelFunc
}

// listenerSlot holds a listener and an id so Subscribe can return a working unsub.
type listenerSlot struct {
	id int
	l  Listener
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
	id := a.nextListenerID
	a.nextListenerID++
	a.listeners = append(a.listeners, listenerSlot{id: id, l: l})
	return func() {
		a.listenersMu.Lock()
		defer a.listenersMu.Unlock()
		for i := range a.listeners {
			if a.listeners[i].id == id {
				last := len(a.listeners) - 1
				if i != last {
					a.listeners[i] = a.listeners[last]
				}
				a.listeners = a.listeners[:last]
				return
			}
		}
	}
}

func (a *Agent) emit(e Event) {
	a.listenersMu.RLock()
	defer a.listenersMu.RUnlock()
	for _, slot := range a.listeners {
		slot.l(e)
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

// Prompt sends a new user message and runs one or more LLM turns,
// executing tools in between turns when requested by the model.
func (a *Agent) Prompt(ctx context.Context, content string) error {
	// First, append the user message.
	turnID := time.Now().UTC().Format(time.RFC3339Nano)
	userMsg := AgentMessage{
		ID:      "user-" + turnID,
		Role:    RoleUser,
		Content: content,
	}
	a.AppendMessage(userMsg)

	a.emit(Event{Type: EventAgentStart, AgentID: a.id})

	var lastErr error
	firstTurn := true

	for {
		currentTurnID := time.Now().UTC().Format(time.RFC3339Nano)

		a.emit(Event{Type: EventTurnStart, AgentID: a.id, TurnID: currentTurnID})

		if firstTurn {
			a.emit(Event{Type: EventMessageStart, AgentID: a.id, TurnID: currentTurnID, Message: &userMsg})
			a.emit(Event{Type: EventMessageEnd, AgentID: a.id, TurnID: currentTurnID, Message: &userMsg})
			firstTurn = false
		}

		ctxTurn, cancel := context.WithCancel(ctx)
		a.currentCancel = cancel
		a.state.IsStreaming = true

		// Prepare LLM context for this turn.
		messages := a.transformContext(a.state.Messages, ctxTurn)
		llmMessages := a.convertToLlm(messages)
		llmCtx := llm.Context{Messages: llmMessages}
		// Clear any pending tool calls from a previous turn.
		a.state.PendingToolCalls = nil

		opts := &llm.Options{Tools: agentToolsToLlm(a.state.Tools)}
		events, err := a.streamFn(ctxTurn, a.state.Model, llmCtx, opts)
		if err != nil {
			a.state.Error = err
			lastErr = err
			a.emit(Event{Type: EventTurnEnd, AgentID: a.id, TurnID: currentTurnID, Error: err})
			a.state.IsStreaming = false
			a.currentCancel = nil
			break
		}

		assistant := AgentMessage{
			ID:      "assistant-" + currentTurnID,
			Role:    RoleAssistant,
			Content: "",
		}
		a.state.StreamMessage = &assistant
		a.emit(Event{Type: EventMessageStart, AgentID: a.id, TurnID: currentTurnID, Message: &assistant})

		for ev := range events {
			switch ev.Type {
			case llm.EventTextDelta:
				assistant.Content += ev.TextDelta
				a.emit(Event{
					Type:     EventMessageUpdate,
					AgentID:  a.id,
					TurnID:   currentTurnID,
					Message:  &assistant,
					LlmEvent: &ev,
				})
			case llm.EventToolCallStart, llm.EventToolCallDelta, llm.EventToolCallEnd:
				if ev.ToolCall != nil {
					a.upsertPendingToolCall(*ev.ToolCall)
				}
			case llm.EventError:
				a.state.Error = ev.Error
				lastErr = ev.Error
				assistant.IsError = true
			case llm.EventDone:
				// handled after loop
			}
		}

		a.state.IsStreaming = false
		a.currentCancel = nil

		// Finalize assistant message for this turn.
		a.state.StreamMessage = nil
		if len(a.state.PendingToolCalls) > 0 {
			assistant.LlmMessage = &llm.Message{
				Role:     llm.RoleAssistant,
				Content:  []llm.ContentBlock{{Type: "text", Text: assistant.Content}},
				ToolCall: &a.state.PendingToolCalls[0],
			}
		}
		a.AppendMessage(assistant)
		a.emit(Event{Type: EventMessageEnd, AgentID: a.id, TurnID: currentTurnID, Message: &assistant})
		a.emit(Event{Type: EventTurnEnd, AgentID: a.id, TurnID: currentTurnID})

		// If an error occurred, stop here.
		if lastErr != nil {
			break
		}

		// If the model did not request any tools, we're done.
		if len(a.state.PendingToolCalls) == 0 {
			break
		}

		// Execute tools requested in this turn, append toolResult messages,
		// then continue the loop for another LLM turn so it can see the results.
		a.executePendingTools(ctx)
	}

	a.emit(Event{Type: EventAgentEnd, AgentID: a.id, Error: lastErr})
	return lastErr
}

// Abort cancels the current LLM call if one is active.
func (a *Agent) Abort() {
	if a.currentCancel != nil {
		a.currentCancel()
	}
}

// defaultConvertToLlm converts AgentMessage to llm.Message by mapping roles and content.
// When an assistant message has LlmMessage set (e.g. after a tool-call turn), that is used so tool_calls are preserved for the API.
func defaultConvertToLlm(msgs []AgentMessage) []llm.Message {
	out := make([]llm.Message, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case RoleSystem, RoleUser, RoleAssistant, RoleTool:
			if m.Role == RoleAssistant && m.LlmMessage != nil {
				out = append(out, *m.LlmMessage)
				continue
			}
			role := llm.Role(string(m.Role))
			msg := llm.Message{
				Role: role,
				Content: []llm.ContentBlock{
					{Type: "text", Text: m.Content},
				},
			}
			if m.Role == RoleTool {
				msg.ToolCallID = m.ToolCallID
			}
			out = append(out, msg)
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

// agentToolsToLlm converts AgentTools to llm.Tool slice for provider options.
func agentToolsToLlm(tools []AgentTool) []llm.Tool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]llm.Tool, 0, len(tools))
	for _, t := range tools {
		out = append(out, llm.Tool{
			Name:        t.Name(),
			Description: t.Description(),
			JSONSchema:  t.JSONSchema(),
		})
	}
	return out
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

// upsertPendingToolCall tracks the latest version of a ToolCall by ID.
func (a *Agent) upsertPendingToolCall(call llm.ToolCall) {
	for i := range a.state.PendingToolCalls {
		if a.state.PendingToolCalls[i].ID == call.ID {
			a.state.PendingToolCalls[i] = call
			return
		}
	}
	a.state.PendingToolCalls = append(a.state.PendingToolCalls, call)
}

func (a *Agent) findTool(name string) AgentTool {
	for _, t := range a.state.Tools {
		if t.Name() == name {
			return t
		}
	}
	return nil
}

// executePendingTools runs all pending tool calls and appends toolResult messages.
func (a *Agent) executePendingTools(ctx context.Context) {
	calls := a.state.PendingToolCalls
	a.state.PendingToolCalls = nil

	for _, call := range calls {
		tool := a.findTool(call.Name)
		if tool == nil {
			toolMsg := AgentMessage{
				ID:         "tool-" + call.ID,
				Role:       RoleTool,
				Content:    "unknown tool: " + call.Name,
				ToolCallID: call.ID,
				IsError:    true,
			}
			a.AppendMessage(toolMsg)
			a.emit(Event{
				Type:     EventToolExecutionEnd,
				AgentID:  a.id,
				ToolName: call.Name,
				Message:  &toolMsg,
				Error:    llm.ErrUnknownProvider(call.Name),
			})
			continue
		}

		a.emit(Event{
			Type:     EventToolExecutionStart,
			AgentID:  a.id,
			ToolName: tool.Name(),
		})

		args := call.Arguments
		// Some APIs return arguments as a JSON string (double-encoded); unwrap once.
		if len(args) >= 2 && args[0] == '"' {
			var s string
			if err := json.Unmarshal(args, &s); err == nil {
				args = json.RawMessage(s)
			}
		}

		result, err := tool.Execute(ctx, call.ID, args, nil)
		if err != nil {
			result = ToolResult{
				Content: err.Error(),
				IsError: true,
			}
		}

		toolMsg := AgentMessage{
			ID:         "tool-" + call.ID,
			Role:       RoleTool,
			Content:    result.Content,
			ToolCallID: call.ID,
			IsError:    result.IsError,
			Metadata:   result.Metadata,
		}
		a.AppendMessage(toolMsg)

		a.emit(Event{
			Type:     EventToolExecutionEnd,
			AgentID:  a.id,
			ToolName: tool.Name(),
			Message:  &toolMsg,
			Error:    err,
		})
	}
}
