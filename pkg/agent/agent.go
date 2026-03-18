package agent

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"acgo/pkg/keys"
	"acgo/pkg/llm"
	"acgo/pkg/log"
	"acgo/pkg/memory"
)

// Options configures an Agent instance.
type Options struct {
	InitialState     State
	StreamFn         llm.StreamFunc
	ConvertToLlm     func([]Message) []llm.Message
	TransformContext func([]Message, context.Context) []Message

	// MemoryWriter is optional. When set, Agent will automatically write
	// long-term dialogue memories after each user->assistant exchange.
	MemoryWriter MemoryWriter
}

// MemoryWriter stores conversation memories for long-term retrieval.
type MemoryWriter interface {
	WriteDialogue(ctx context.Context, sessionID string, userText string, assistantText string) error
}

// Agent coordinates LLM calls, tools and state updates.
type Agent struct {
	id       string
	state    State
	streamFn llm.StreamFunc

	convertToLlm     func([]Message) []llm.Message
	transformContext func([]Message, context.Context) []Message

	listenersMu    sync.RWMutex
	listeners      []listenerSlot
	nextListenerID int

	queueMu       sync.Mutex // protects SteeringQueue and FollowUpQueue
	currentCancel context.CancelFunc

	memoryWriter MemoryWriter
}

// listenerSlot holds a listener and an id so Subscribe can return a working unsub.
type listenerSlot struct {
	id int
	l  Listener
}

// New creates a new Agent with the given options.
func New(id string, opts Options) *Agent {
	a := &Agent{
		id:           id,
		state:        opts.InitialState,
		memoryWriter: opts.MemoryWriter,
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
func (a *Agent) State() State {
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
func (a *Agent) SetThinkingLevel(level keys.ThinkingLevel) {
	a.state.ThinkingLevel = level
}

// SetTools replaces the tool set.
func (a *Agent) SetTools(tools []AgentTool) {
	a.state.Tools = tools
}

// AppendMessage appends a message to the history.
func (a *Agent) AppendMessage(msg Message) {
	a.state.Messages = append(a.state.Messages, msg)
}

// ClearMessages clears all messages.
func (a *Agent) ClearMessages() {
	a.state.Messages = nil
}

// ReplaceMessages replaces the entire message history with a copy of msgs.
// Callers can use this to load a session or batch-replace history.
func (a *Agent) ReplaceMessages(msgs []Message) {
	if msgs == nil {
		a.state.Messages = nil
		return
	}
	a.state.Messages = append([]Message(nil), msgs...)
}

// SetError sets the agent's error state (e.g. after a failed LLM or tool call).
func (a *Agent) SetError(err error) {
	a.state.Error = err
	a.state.LastErrorKind = ClassifyError(err)
}

// ClearError clears the agent's error state.
func (a *Agent) ClearError() {
	a.state.Error = nil
	a.state.LastErrorKind = ErrKindNone
}

// WaitForIdle blocks until the agent is not streaming (current turn finished or idle).
// Returns ctx.Err() if the context is cancelled before the agent becomes idle.
// Useful in UI or tests when waiting for the current stream to complete.
func (a *Agent) WaitForIdle(ctx context.Context) error {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		if !a.state.IsStreaming {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// Reset clears messages, error state, and queues.
func (a *Agent) Reset() {
	a.state.Messages = nil
	a.state.Error = nil
	a.state.LastErrorKind = ErrKindNone
	a.state.StreamMessage = nil
	a.state.PendingToolCalls = nil
	a.queueMu.Lock()
	a.state.SteeringQueue = nil
	a.state.FollowUpQueue = nil
	a.queueMu.Unlock()
}

// EnqueueSteering adds a message to the steering queue. When the agent is busy,
// callers can enqueue; after the current turn ends, steering messages are consumed first.
func (a *Agent) EnqueueSteering(msg Message) {
	a.queueMu.Lock()
	defer a.queueMu.Unlock()
	a.state.SteeringQueue = append(a.state.SteeringQueue, msg)
}

// EnqueueFollowUp adds a message to the follow-up queue. Consumed after SteeringQueue is empty.
func (a *Agent) EnqueueFollowUp(msg Message) {
	a.queueMu.Lock()
	defer a.queueMu.Unlock()
	a.state.FollowUpQueue = append(a.state.FollowUpQueue, msg)
}

// drainOneFromQueues removes and returns one message: steering first, then follow-up.
// Caller must not hold queueMu.
func (a *Agent) drainOneFromQueues() *Message {
	a.queueMu.Lock()
	defer a.queueMu.Unlock()
	if len(a.state.SteeringQueue) > 0 {
		msg := a.state.SteeringQueue[0]
		a.state.SteeringQueue = a.state.SteeringQueue[1:]
		return &msg
	}
	if len(a.state.FollowUpQueue) > 0 {
		msg := a.state.FollowUpQueue[0]
		a.state.FollowUpQueue = a.state.FollowUpQueue[1:]
		return &msg
	}
	return nil
}

// Prompt sends a new user message and runs one or more LLM turns,
// executing tools in between turns when requested by the model.
// When the current turn ends (no more tool calls), queued steering and follow-up
// messages are consumed (steering first, then follow-up) and processed before returning.
func (a *Agent) Prompt(ctx context.Context, content string) error {
	turnID := time.Now().UTC().Format(time.RFC3339Nano)
	userMsg := Message{
		ID:      "user-" + turnID,
		Role:    keys.AgentRoleUser,
		Content: content,
	}
	a.AppendMessage(userMsg)
	a.emit(Event{Type: EventAgentStart, AgentID: a.id})

	var lastErr error
	firstTurn := true

	for {
		currentTurnID := time.Now().UTC().Format(time.RFC3339Nano)
		a.emit(Event{Type: EventTurnStart, AgentID: a.id, TurnID: currentTurnID})

		var userTurnMsg *Message
		if firstTurn {
			a.emitUserMessage(currentTurnID, &userMsg)
			userTurnMsg = &userMsg
			firstTurn = false
		} else {
			userTurnMsg = a.emitNextQueuedMessage(currentTurnID)
			if userTurnMsg == nil {
				break
			}
		}

		lastErr = a.runLLMTurnsUntilDone(ctx, currentTurnID)
		if lastErr != nil {
			break
		}

		// Long-term memory write: user message -> final assistant content.
		if a.memoryWriter != nil && userTurnMsg != nil {
			sessionID, _ := memory.SessionIDFromContext(ctx)
			assistantText := lastAssistantText(a.state.Messages)
			if assistantText != "" || userTurnMsg.Content != "" {
				if err := a.memoryWriter.WriteDialogue(ctx, sessionID, userTurnMsg.Content, assistantText); err != nil {
					log.Debugf("[memory] write dialogue err=%v", err)
				}
			}
		}
	}

	kind := ClassifyError(lastErr)
	a.emit(Event{Type: EventAgentEnd, AgentID: a.id, Error: lastErr, ErrorKind: kind})
	return WrapAgentError(lastErr, kind)
}

// emitUserMessage emits MessageStart and MessageEnd for a user message.
func (a *Agent) emitUserMessage(turnID string, msg *Message) {
	a.emit(Event{Type: EventMessageStart, AgentID: a.id, TurnID: turnID, Message: msg})
	a.emit(Event{Type: EventMessageEnd, AgentID: a.id, TurnID: turnID, Message: msg})
}

// emitNextQueuedMessage drains one message from steering or follow-up queue, appends it, and emits events.
// Returns the consumed message or nil if both queues were empty.
func (a *Agent) emitNextQueuedMessage(turnID string) *Message {
	next := a.drainOneFromQueues()
	if next == nil {
		return nil
	}
	a.AppendMessage(*next)
	a.emitUserMessage(turnID, next)
	return next
}

// runLLMTurnsUntilDone runs stream turns and tool execution until no more tool calls or an error.
func (a *Agent) runLLMTurnsUntilDone(ctx context.Context, turnID string) error {
	for {
		streamErr, hasToolCalls := a.runOneStreamTurn(ctx, turnID)
		if streamErr != nil {
			return streamErr
		}
		if !hasToolCalls {
			return nil
		}
		a.executePendingTools(ctx)
	}
}

// runOneStreamTurn performs one LLM stream call: build context, stream, process events, append assistant, emit done.
// Returns (error if any, whether there are pending tool calls to execute).
func (a *Agent) runOneStreamTurn(ctx context.Context, turnID string) (err error, hasToolCalls bool) {
	ctxTurn, cancel := context.WithCancel(ctx)
	a.currentCancel = cancel
	a.state.IsStreaming = true
	defer func() {
		a.state.IsStreaming = false
		a.currentCancel = nil
	}()

	messages := a.transformContext(a.state.Messages, ctxTurn)
	llmMessages := a.convertToLlm(messages)
	llmCtx := llm.Context{Go: ctxTurn, Messages: llmMessages}
	a.state.PendingToolCalls = nil

	opts := &llm.Options{
		Tools:           agentToolsToLlm(a.state.Tools),
		ReasoningEffort: a.state.ThinkingLevel,
	}

	a.debugLogLLMRequest(turnID, llmCtx, a.state.Model, opts)
	events, streamErr := a.streamFn(llmCtx, a.state.Model, opts)
	if streamErr != nil {
		kind := ClassifyError(streamErr)
		a.state.Error = streamErr
		a.state.LastErrorKind = kind
		a.emit(Event{Type: EventTurnEnd, AgentID: a.id, TurnID: turnID, Error: streamErr, ErrorKind: kind})
		return streamErr, false
	}

	assistant := Message{ID: "assistant-" + turnID, Role: keys.AgentRoleAssistant}
	a.state.StreamMessage = &assistant
	a.emit(Event{Type: EventMessageStart, AgentID: a.id, TurnID: turnID, Message: &assistant})

	lastDone, lastErr := a.processStreamEvents(events, &assistant, turnID)
	a.state.StreamMessage = nil

	if len(a.state.PendingToolCalls) > 0 {
		assistant.LlmMessage = &llm.Message{
			Role:     llm.RoleAssistant,
			Content:  []llm.ContentBlock{{Type: "text", Text: assistant.Content}},
			Thinking: assistant.Thinking,
			ToolCall: &a.state.PendingToolCalls[0],
		}
	}
	a.AppendMessage(assistant)

	if lastDone != nil {
		a.state.LastStopReason = lastDone.StopReason
		if lastDone.Usage != nil {
			a.state.LastUsage = lastDone.Usage
		} else {
			a.state.LastUsage = nil
		}
	}
	turnEndKind := ClassifyError(lastErr)
	a.emit(Event{Type: EventMessageEnd, AgentID: a.id, TurnID: turnID, Message: &assistant})
	a.emit(Event{Type: EventTurnEnd, AgentID: a.id, TurnID: turnID, LlmEvent: lastDone, Error: lastErr, ErrorKind: turnEndKind})
	if lastErr != nil {
		a.state.Error = lastErr
		a.state.LastErrorKind = turnEndKind
		return lastErr, false
	}
	return nil, len(a.state.PendingToolCalls) > 0
}

func (a *Agent) debugLogLLMRequest(turnID string, llmCtx llm.Context, model llm.Model, opts *llm.Options) {
	// Only emits at debug level; safe to call unconditionally.
	const maxMsgChars = 240
	const maxSystemPromptChars = 1200

	var toolNames []string
	if opts != nil {
		for _, t := range opts.Tools {
			toolNames = append(toolNames, t.Name)
		}
	}

	var b strings.Builder
	b.WriteString("llm request:\n")
	b.WriteString("  turn_id: " + turnID + "\n")
	b.WriteString("  model: " + strings.TrimSpace(model.Provider) + "/" + strings.TrimSpace(model.ID) + "\n")
	if opts != nil {
		b.WriteString("  reasoning_effort: " + string(opts.ReasoningEffort) + "\n")
	}
	sys := strings.TrimSpace(a.state.SystemPrompt)
	sysPreview := sys
	if len(sysPreview) > maxSystemPromptChars {
		sysPreview = sysPreview[:maxSystemPromptChars] + "…"
	}
	b.WriteString("  system_prompt_chars: " + strconv.Itoa(len(sys)) + "\n")
	if sysPreview != "" {
		b.WriteString("  system_prompt: " + strconv.Quote(sysPreview) + "\n")
	} else {
		b.WriteString("  system_prompt: (empty)\n")
	}
	if len(toolNames) > 0 {
		b.WriteString("  tools: " + strings.Join(toolNames, ", ") + "\n")
	} else {
		b.WriteString("  tools: (none)\n")
	}
	b.WriteString("  messages:\n")
	for i, m := range llmCtx.Messages {
		role := string(m.Role)
		snippet := ""
		if len(m.Content) > 0 {
			// Prefer text snippets; ignore non-text blocks for logging.
			for _, blk := range m.Content {
				if blk.Type == "text" && strings.TrimSpace(blk.Text) != "" {
					snippet = strings.TrimSpace(blk.Text)
					break
				}
			}
		}
		if snippet == "" && m.ToolResult != nil {
			for _, blk := range m.ToolResult.Content {
				if blk.Type == "text" && strings.TrimSpace(blk.Text) != "" {
					snippet = strings.TrimSpace(blk.Text)
					break
				}
			}
		}
		if len(snippet) > maxMsgChars {
			snippet = snippet[:maxMsgChars] + "…"
		}

		line := "    - [" + strconv.Itoa(i) + "] " + role
		if m.ToolCall != nil && strings.TrimSpace(m.ToolCall.Name) != "" {
			line += " tool_call=" + strings.TrimSpace(m.ToolCall.Name)
		}
		if m.ToolResult != nil && strings.TrimSpace(m.ToolResult.ToolCallID) != "" {
			line += " tool_result_call_id=" + strings.TrimSpace(m.ToolResult.ToolCallID)
		}
		if snippet != "" {
			line += " text=" + strconv.Quote(snippet)
		}
		b.WriteString(line + "\n")
	}

	log.Debug(strings.TrimSuffix(b.String(), "\n"))
}

// processStreamEvents consumes the event channel and updates assistant state; emits MessageUpdate events.
func (a *Agent) processStreamEvents(events <-chan llm.Event, assistant *Message, turnID string) (lastDone *llm.Event, lastErr error) {
	for ev := range events {
		switch ev.Type {
		case llm.EventTextDelta:
			assistant.Content += ev.TextDelta
			a.emit(Event{Type: EventMessageUpdate, AgentID: a.id, TurnID: turnID, Message: assistant, LlmEvent: &ev})
		case llm.EventThinkingStart:
			evCopy := ev
			a.emit(Event{Type: EventMessageUpdate, AgentID: a.id, TurnID: turnID, Message: assistant, LlmEvent: &evCopy})
		case llm.EventThinkingDelta:
			assistant.Thinking += ev.ThinkingDelta
			evCopy := ev
			a.emit(Event{Type: EventMessageUpdate, AgentID: a.id, TurnID: turnID, Message: assistant, LlmEvent: &evCopy})
		case llm.EventThinkingEnd:
			evCopy := ev
			a.emit(Event{Type: EventMessageUpdate, AgentID: a.id, TurnID: turnID, Message: assistant, LlmEvent: &evCopy})
		case llm.EventToolCallStart, llm.EventToolCallDelta, llm.EventToolCallEnd:
			if ev.ToolCall != nil {
				a.upsertPendingToolCall(*ev.ToolCall)
			}
		case llm.EventError:
			a.state.Error = ev.Error
			a.state.LastErrorKind = ErrKindLLM
			lastErr = ev.Error
			assistant.IsError = true
		case llm.EventDone:
			evCopy := ev
			lastDone = &evCopy
		}
	}
	return lastDone, lastErr
}

// Abort cancels the current LLM call if one is active.
func (a *Agent) Abort() {
	if a.currentCancel != nil {
		a.currentCancel()
	}
}

// defaultStreamFn looks up the provider for the given model and calls its Stream function.
func defaultStreamFn(callCtx llm.Context, model llm.Model, opts *llm.Options) (<-chan llm.Event, error) {
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
	return provider.Stream(callCtx, model, opts)
}

// upsertPendingToolCall tracks the latest version of a ToolCall by ID.
// Stored Arguments are always normalized so they are non-nil and usable for execution.
func (a *Agent) upsertPendingToolCall(call llm.ToolCall) {
	normalized := llm.ToolCall{
		ID:        call.ID,
		Name:      call.Name,
		Arguments: llm.NormalizeToolCallArguments(call.Arguments),
	}
	for i := range a.state.PendingToolCalls {
		if a.state.PendingToolCalls[i].ID == call.ID {
			a.state.PendingToolCalls[i] = normalized
			return
		}
	}
	a.state.PendingToolCalls = append(a.state.PendingToolCalls, normalized)
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
			args := llm.NormalizeToolCallArguments(call.Arguments)
			toolMsg := Message{
				ID:         "tool-" + call.ID,
				Role:       keys.AgentRoleTool,
				Content:    "unknown tool: " + call.Name,
				ToolCallID: call.ID,
				IsError:    true,
			}
			a.AppendMessage(toolMsg)
			a.emit(Event{
				Type:       EventToolExecutionEnd,
				AgentID:    a.id,
				ToolName:   call.Name,
				ToolCallID: call.ID,
				ToolArgs:   args,
				Message:    &toolMsg,
				Error:      llm.ErrUnknownProvider(call.Name),
				ErrorKind:  ErrKindTool,
			})
			continue
		}

		args := llm.NormalizeToolCallArguments(call.Arguments)
		a.emit(Event{
			Type:       EventToolExecutionStart,
			AgentID:    a.id,
			ToolName:   tool.Name(),
			ToolCallID: call.ID,
			ToolArgs:   args,
		})

		result, err := tool.Execute(ctx, call.ID, args, nil)
		if err != nil {
			result = ToolResult{
				Content: err.Error(),
				IsError: true,
			}
		}

		toolMsg := Message{
			ID:         "tool-" + call.ID,
			Role:       keys.AgentRoleTool,
			Content:    result.Content,
			ToolCallID: call.ID,
			IsError:    result.IsError,
			Metadata:   result.Metadata,
		}
		a.AppendMessage(toolMsg)

		a.emit(Event{
			Type:       EventToolExecutionEnd,
			AgentID:    a.id,
			ToolName:   tool.Name(),
			ToolCallID: call.ID,
			ToolArgs:   args,
			Message:    &toolMsg,
			Error:      err,
			ErrorKind:  ErrKindTool,
		})
	}
}

func lastAssistantText(msgs []Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == keys.AgentRoleAssistant {
			return msgs[i].Content
		}
	}
	return ""
}

// State AgentState holds the mutable state of an Agent instance.
type State struct {
	SystemPrompt  string
	Model         llm.Model
	ThinkingLevel keys.ThinkingLevel
	Tools         []AgentTool
	Messages      []Message

	IsStreaming      bool
	StreamMessage    *Message
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
	SteeringQueue []Message

	// FollowUpQueue holds follow-up messages; consumed after SteeringQueue is empty.
	FollowUpQueue []Message
}
