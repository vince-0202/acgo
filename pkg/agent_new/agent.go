package agent_new

import (
	"context"
	"strings"

	"github.com/vince-0202/acgo/pkg/communi"
	"github.com/vince-0202/acgo/pkg/errors"
	"github.com/vince-0202/acgo/pkg/keys"
	"github.com/vince-0202/acgo/pkg/llm"
	"github.com/vince-0202/acgo/pkg/utils"
)

// Agent coordinates LLM calls, tools and state updates.
type Agent struct {
	id              string
	model           llm.Model
	provider        llm.Provider
	state           State
	context         *Context
	toolManager     *toolsManager
	turnManager     *turnManager
	listenerManager *listenerManager
	queueManager    *queueManager
	subAgentManager *SubAgentManager
	currentCancel   context.CancelFunc
}

func (a *Agent) ID() string {
	if a == nil {
		return ""
	}
	return a.id
}

// Prompt sends a new user message and runs one or more LLM turns,
// executing tools in between turns when requested by the model.
// When the current turn ends (no more tool calls), queued steering and follow-up
// messages are consumed (steering first, then follow-up) and processed before returning.
func (a *Agent) Prompt(ctx context.Context, content string) error {
	turnID := utils.SnowflakeIDString()
	return a.promptWithInitialMessage(ctx, communi.NewUserMessage(turnID, content))
}

func (a *Agent) promptWithInitialMessage(ctx context.Context, initialMsg communi.Message) error {
	a.turnManager.reset()
	a.emit(NewEvent(
		WithEventType(EventAgentStart),
		WithEventAgent(a),
	))
	a.ensureSystemPromptMessage()
	a.turnManager.startAgentTurn(ctx, a, &initialMsg)
	a.emit(NewEvent(
		WithEventType(EventAgentEnd),
		WithEventAgent(a),
		WithEventError(a.turnManager.lastTurnError),
	))
	if a.turnManager.lastTurnError == nil {
		return nil
	}
	return a.turnManager.lastTurnError
}

func (a *Agent) ensureSystemPromptMessage() {
	p := strings.TrimSpace(a.context.Prompt)
	if p == "" {
		return
	}
	msgs := a.context.Messages
	if len(msgs) > 0 && msgs[0].Role == keys.AgentRoleSystem {
		return
	}
	a.context.ReplaceMessages(append([]communi.Message{communi.NewSystemMessageWithoutId(p)}, msgs...))
}

func (a *Agent) emit(e Event) {
	a.listenerManager.emit(e, a.Abort)
}

func (a *Agent) Emit(e Event) {
	a.emit(e)
}

// Abort cancels the current LLM call if one is active.
func (a *Agent) Abort() {
	if a.currentCancel != nil {
		a.currentCancel()
	}
}

// emitUserMessage emits MessageStart and MessageEnd for a user message.
func (a *Agent) emitUserMessage(turnID string, msg *communi.Message) {
	a.emit(NewEvent(
		WithEventType(EventMessageStart),
		WithEventAgent(a),
		WithEventTurnId(turnID),
		WithEventMessage(msg),
	))
	a.context.AppendMessage(*msg)
	a.emit(NewEvent(
		WithEventType(EventMessageEnd),
		WithEventAgent(a),
		WithEventTurnId(turnID),
		WithEventMessage(msg),
	))
}

// SetThinkingLevel updates the thinking level.
func (a *Agent) SetThinkingLevel(level keys.ThinkingLevel) {
	a.state.ThinkingLevel = level
}

func (a *Agent) Context() *Context {
	return a.context
}

func (a *Agent) ContextManager() ContextRuntime {
	return a.context
}

func (a *Agent) Model() llm.Model {
	return a.model
}

// SetModel updates the model used for subsequent LLM calls.
func (a *Agent) SetModel(model llm.Model) {
	if a == nil {
		return
	}
	a.model = model
}

func (a *Agent) Provider() llm.Provider {
	return a.provider
}

func (a *Agent) State() State {
	return a.state
}

func (a *Agent) Subscribe(l Listener) func() {
	return a.listenerManager.AddListener(l)
}

func (a *Agent) ToolManager() ToolRuntime {
	return a.toolManager
}

func (a *Agent) QueueManager() QueueRuntime {
	return a.queueManager
}

func (a *Agent) SubAgentManager() SubAgentRuntime {
	return a.subAgentManager
}

// Reset clears messages, error state, and queues.
func (a *Agent) Reset() {
	a.context.ClearMessages()
	a.toolManager.ClearPendingTool()
	a.state.clear()
	a.queueManager.Clear()
}

// EnqueueSteering adds a message to the steering queue. When the agent is busy,
// callers can enqueue; after the current turn ends, steering messages are consumed first.
func (a *Agent) EnqueueSteering(msg communi.Message) {
	a.queueManager.EnqueueSteering(msg)
}

// EnqueueFollowUp adds a message to the follow-up queue. Consumed after SteeringQueue is empty.
func (a *Agent) EnqueueFollowUp(msg communi.Message) {
	a.queueManager.EnqueueFollowUp(msg)
}

// State AgentState holds the mutable state of an Agent instance.
type State struct {
	WorkDir       string
	ThinkingLevel keys.ThinkingLevel
	IsStreaming   bool
	StreamMessage *communi.Message
	Error         *errors.AgentError

	// LastUsage captures the most recent token usage reported by the LLM
	// provider for a completed turn, if available.
	LastUsage *communi.Usage

	// LastStopReason records the last completion stop reason reported by
	// the provider (e.g. "stop", "length", "toolUse", "error", "aborted").
	LastStopReason string
}

func (s *State) clear() {
	s.Error = nil
	s.StreamMessage = nil
	s.IsStreaming = false
}
