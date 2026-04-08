package agent

import (
	"context"
	"strings"
	"time"

	"github.com/vince-0202/acgo/pkg/communi"
	"github.com/vince-0202/acgo/pkg/errors"
	"github.com/vince-0202/acgo/pkg/harness"
	"github.com/vince-0202/acgo/pkg/utils"

	"github.com/vince-0202/acgo/pkg/keys"
	"github.com/vince-0202/acgo/pkg/llm"
)

// Options configures an Agent instance.
type Options struct {
	WorkDir     string
	Model       llm.Model
	Provider    llm.Provider
	UseTools    []harness.Tool
	Permissions harness.PermissionsOptions
	// ContextTrim configures deterministic trimming and auto LLM compaction thresholds for ContextController.
	ContextTrim  harness.ContextOptions
	InitialState State
	// MemoryWriter is optional; when nil, harness.MemoryController.Load uses memory.DefaultManager().
	MemoryWriter harness.MemoryWriter
	// DisableSubAgentTool when true skips registering the sub_agent tool (used for child agents).
	DisableSubAgentTool bool
}

// Agent coordinates LLM calls, tools and state updates.
type Agent struct {
	id string
	//all controllers
	contextController  *harness.ContextController
	memoryController   *harness.MemoryController
	toolController     *harness.ToolController
	skillsController   *harness.SkillsController
	subAgentController *SubAgentController
	permissionCtrl     *harness.PermissionController

	Model    llm.Model
	Provider llm.Provider
	state    State

	listenerManager *listenerManager
	queueManager    *QueueManager

	currentCancel context.CancelFunc
}

const planModeInstructionPrompt = "Plan mode is active. Your final response must be a concrete coding plan only (steps, scope, and verification), not code changes. Do not call task-editing tools such as write/edit, and do not perform mutating operations."

func (a *Agent) Id() string {
	return a.id
}

// SubAgentController returns the manager for delegated sub-agents (child agents).
func (a *Agent) SubAgentController() *SubAgentController {
	return a.subAgentController
}

// New creates a new Agent with the given options.
func New(id string, opts Options) *Agent {
	a := &Agent{
		id:       id,
		Provider: opts.Provider,
		Model:    opts.Model,
		state:    opts.InitialState,
		listenerManager: &listenerManager{
			listeners: make([]listenerSlot, 0),
		},
		queueManager: &QueueManager{
			SteeringQueue: make([]communi.Message, 0),
			FollowUpQueue: make([]communi.Message, 0),
		},
	}

	//register the other controller
	//first registry context controller
	a.contextController = harness.NewContextController(id, opts.WorkDir)
	ct := opts.ContextTrim
	a.contextController.Options = &ct
	a.contextController.SetCompactLLM(opts.Provider, opts.Model)

	a.skillsController = harness.NewSkillsController(opts.WorkDir)
	a.skillsController.Load()

	a.permissionCtrl = harness.NewPermissionController(
		opts.Permissions.Mode,
		opts.Permissions.Rules,
		opts.Permissions.ConfirmHook,
	)
	a.toolController = harness.NewToolController(a.id, a.contextController, a.emit, a.permissionCtrl,
		opts.UseTools...,
	)
	a.memoryController = harness.NewMemoryController(opts.MemoryWriter)
	a.memoryController.Load()

	a.subAgentController = NewSubAgentController(a)
	if !opts.DisableSubAgentTool {
		a.toolController.RegistryTool(newSubAgentTool(a.subAgentController))
	}

	a.LoadContext()
	a.state.WorkDir = opts.WorkDir
	return a
}

// State returns a copy of the current state.
func (a *Agent) State() State {
	return a.state
}

// Subscribe registers a listener for events. It returns an unsubscribe function.
func (a *Agent) Subscribe(l communi.Listener) func() {
	return a.listenerManager.AddListener(l)
}

func (a *Agent) emit(e communi.AgentEvent) {
	a.listenerManager.emit(e)
}

// SetModel updates the model.
func (a *Agent) SetModel(m llm.Model) {
	a.Model = m
	if a.contextController != nil {
		a.contextController.SetCompactLLM(a.Provider, m)
	}
}

// CompactContext runs LLM-based context compaction (same as /compact). transcriptPath is optional suffix for full transcript.
func (a *Agent) CompactContext(ctx context.Context, transcriptPath string) error {
	if a == nil || a.contextController == nil {
		return nil
	}
	return a.contextController.TrimMessage(ctx, &harness.TrimMessageOptions{
		Mode:           harness.TrimModeLLMCompact,
		Force:          true,
		TranscriptPath: strings.TrimSpace(transcriptPath),
	})
}

func (a *Agent) SetPermissionConfirmHook(hook harness.PermissionConfirmHook) {
	if a == nil || a.permissionCtrl == nil {
		return
	}
	a.permissionCtrl.SetConfirmHook(hook)
	if a.subAgentController != nil {
		a.subAgentController.setConfirmHookOnAllChildren(hook)
	}
}

// SetPermissionMode updates runtime permission mode and syncs existing sub-agents.
func (a *Agent) SetPermissionMode(mode harness.PermissionMode) error {
	if a == nil || a.permissionCtrl == nil {
		return nil
	}
	if err := a.permissionCtrl.SetMode(mode); err != nil {
		return err
	}
	if a.subAgentController != nil {
		if err := a.subAgentController.setPermissionModeOnAllChildren(a.permissionCtrl.Mode()); err != nil {
			return err
		}
	}
	return nil
}

// PermissionMode returns the current effective runtime permission mode.
func (a *Agent) PermissionMode() harness.PermissionMode {
	if a == nil || a.permissionCtrl == nil {
		return harness.PermissionModeDefault
	}
	return a.permissionCtrl.Mode()
}

// SetThinkingLevel updates the thinking level.
func (a *Agent) SetThinkingLevel(level keys.ThinkingLevel) {
	a.state.ThinkingLevel = level
}

// SetError sets the agent's error state (e.g. after a failed LLM or tool call).
func (a *Agent) SetError(err error) {
	a.state.Error = err
	a.state.LastErrorKind = errors.ClassifyError(err)
}

// ClearError clears the agent's error state.
func (a *Agent) ClearError() {
	a.state.Error = nil
	a.state.LastErrorKind = errors.ErrKindNone
}

// WaitForIdle blocks until the agent is not streaming (current turn finished or idle).
// Returns contextController.Err() if the context is cancelled before the agent becomes idle.
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
	a.contextController.ClearMessages()
	a.toolController.CleanPendingTool()
	a.state.Error = nil
	a.state.LastErrorKind = errors.ErrKindNone
	a.state.StreamMessage = nil
	a.queueManager.Clean()

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

// Prompt sends a new user message and runs one or more LLM turns,
// executing tools in between turns when requested by the model.
// When the current turn ends (no more tool calls), queued steering and follow-up
// messages are consumed (steering first, then follow-up) and processed before returning.
func (a *Agent) Prompt(ctx context.Context, content string) error {
	turnID := utils.SnowflakeIDString()
	a.ensureSystemPromptMessage()
	userMsg := communi.NewUserMessage(turnID, content)
	a.contextController.AppendMessage(userMsg)
	a.emit(communi.AgentEvent{Type: communi.EventAgentStart, AgentID: a.id})

	var lastErr error
	firstTurn := true

	for {
		currentTurnID := utils.SnowflakeIDString()
		a.emit(communi.AgentEvent{Type: communi.EventTurnStart, AgentID: a.id, TurnID: currentTurnID})

		var userTurnMsg *communi.Message
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
		if userTurnMsg != nil {
			go a.memoryController.RecordWithMetaData(ctx, userTurnMsg, map[string]any{
				"turnID": currentTurnID,
				"type":   "userAsk",
			})
		}

		lastErr = a.runLLMTurnsUntilDone(ctx, currentTurnID)
		if lastErr != nil {
			break
		}

		go a.memoryController.RecordWithMetaData(ctx, a.contextController.LastAssistantMessage(), map[string]any{
			"turnID": currentTurnID,
			"type":   "assistant",
		})
	}

	kind := errors.ClassifyError(lastErr)
	a.emit(communi.AgentEvent{Type: communi.EventAgentEnd, AgentID: a.id, Error: lastErr, ErrorKind: kind})
	return errors.WrapAgentError(lastErr, kind)
}

// emitUserMessage emits MessageStart and MessageEnd for a user message.
func (a *Agent) emitUserMessage(turnID string, msg *communi.Message) {
	a.emit(communi.AgentEvent{Type: communi.EventMessageStart, AgentID: a.id, TurnID: turnID, Message: msg})
	a.emit(communi.AgentEvent{Type: communi.EventMessageEnd, AgentID: a.id, TurnID: turnID, Message: msg})
}

// emitNextQueuedMessage drains one message from steering or follow-up queue, appends it, and emits events.
// Returns the consumed message or nil if both queues were empty.
func (a *Agent) emitNextQueuedMessage(turnID string) *communi.Message {
	next := a.queueManager.DrainOneFromQueues()
	if next == nil {
		return nil
	}
	a.contextController.AppendMessage(*next)
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
		a.toolController.Execute(ctx)
	}
}

// runOneStreamTurn performs one LLM stream call: build context, stream, process events, append assistant, emit done.
// Returns (error if any, whether there are pending tool calls to execute).
func (a *Agent) runOneStreamTurn(ctx context.Context, turnID string) (err error, hasToolCalls bool) {
	ctx, cancel := context.WithCancel(ctx)
	a.state.IsStreaming = true
	a.currentCancel = cancel
	defer func() {
		a.state.IsStreaming = false
		a.currentCancel = nil
	}()

	if err := a.contextController.TrimMessage(ctx, nil); err != nil {
		kind := errors.ClassifyError(err)
		a.state.Error = err
		a.state.LastErrorKind = kind
		a.emit(communi.AgentEvent{Type: communi.EventTurnEnd, AgentID: a.id, TurnID: turnID, Error: err, ErrorKind: kind})
		return err, false
	}
	a.toolController.CleanPendingTool()

	opts := &llm.Options{
		Tools:           a.toolController.GetToolSchemas(),
		ReasoningEffort: a.state.ThinkingLevel,
	}
	llmMessages := a.contextController.Messages
	if a.PermissionMode() == harness.PermissionModePlan {
		llmMessages = append([]communi.Message(nil), llmMessages...)
		llmMessages = append(llmMessages, communi.NewSystemMessageWithoutId(planModeInstructionPrompt))
	}

	events, streamErr := a.Provider.Stream(ctx, a.Model, llmMessages, opts)
	if streamErr != nil {
		kind := errors.ClassifyError(streamErr)
		a.state.Error = streamErr
		a.state.LastErrorKind = kind
		a.emit(communi.AgentEvent{Type: communi.EventTurnEnd, AgentID: a.id, TurnID: turnID, Error: streamErr, ErrorKind: kind})
		return streamErr, false
	}

	assistant := communi.NewAssistantMessage(turnID, "")
	a.state.StreamMessage = &assistant
	a.emit(communi.AgentEvent{Type: communi.EventMessageStart, AgentID: a.id, TurnID: turnID, Message: &assistant})

	lastDone, lastErr := a.processStreamEvents(events, &assistant, turnID)
	a.state.StreamMessage = nil

	pendingToolCalls := a.toolController.GetPendingToolCalls()
	if len(pendingToolCalls) > 0 {
		assistant.ToolCalls = append([]communi.ToolCallRequest(nil), pendingToolCalls...)
		assistant.ToolCall = &assistant.ToolCalls[0]
	}
	a.contextController.AppendMessage(assistant)

	if lastDone != nil {
		a.state.LastStopReason = lastDone.StopReason
		if lastDone.Usage != nil {
			a.state.LastUsage = lastDone.Usage
		} else {
			a.state.LastUsage = nil
		}
	}
	turnEndKind := errors.ClassifyError(lastErr)
	a.emit(communi.AgentEvent{Type: communi.EventMessageEnd, AgentID: a.id, TurnID: turnID, Message: &assistant})
	a.emit(communi.AgentEvent{Type: communi.EventTurnEnd, AgentID: a.id, TurnID: turnID, LlmEvent: lastDone, Error: lastErr, ErrorKind: turnEndKind})
	if lastErr != nil {
		a.state.Error = lastErr
		a.state.LastErrorKind = turnEndKind
		return lastErr, false
	}
	return nil, len(pendingToolCalls) > 0
}

// processStreamEvents consumes the event channel and updates assistant state; emits MessageUpdate events.
func (a *Agent) processStreamEvents(events <-chan communi.LLMEvent, assistant *communi.Message, turnID string) (lastDone *communi.LLMEvent, lastErr error) {
	for ev := range events {
		switch ev.Type {
		case communi.EventTextDelta:
			assistant.AppendTextValue(ev.TextDelta)
			a.emit(communi.AgentEvent{Type: communi.EventMessageUpdate, AgentID: a.id, TurnID: turnID, Message: assistant, LlmEvent: &ev})
		case communi.EventThinkingStart:
			evCopy := ev
			a.emit(communi.AgentEvent{Type: communi.EventMessageUpdate, AgentID: a.id, TurnID: turnID, Message: assistant, LlmEvent: &evCopy})
		case communi.EventThinkingDelta:
			assistant.Thinking += ev.ThinkingDelta
			evCopy := ev
			a.emit(communi.AgentEvent{Type: communi.EventMessageUpdate, AgentID: a.id, TurnID: turnID, Message: assistant, LlmEvent: &evCopy})
		case communi.EventThinkingEnd:
			evCopy := ev
			a.emit(communi.AgentEvent{Type: communi.EventMessageUpdate, AgentID: a.id, TurnID: turnID, Message: assistant, LlmEvent: &evCopy})
		case communi.EventToolCallStart, communi.EventToolCallDelta, communi.EventToolCallEnd:
			if ev.ToolCall != nil {
				a.toolController.UpsertPendingToolCall(*ev.ToolCall)
			}
		case communi.EventError:
			a.state.Error = ev.Error
			a.state.LastErrorKind = errors.ErrKindLLM
			lastErr = ev.Error
			assistant.IsError = true
		case communi.EventDone:
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

func (a *Agent) ReplaceMessages(message []communi.Message) {
	a.contextController.ReplaceMessages(message)
}

func (a *Agent) GetMessages() []communi.Message {
	return a.contextController.Messages
}

func (a *Agent) LoadContext() {
	a.contextController.Load()
	a.contextController.AppendSystemPrompt(a.skillsController.SystemPrompt())
}

func (a *Agent) SystemPrompt() string {
	return a.contextController.Prompt
}

func (a *Agent) ensureSystemPromptMessage() {
	p := strings.TrimSpace(a.contextController.Prompt)
	if p == "" {
		return
	}
	msgs := a.contextController.Messages
	if len(msgs) > 0 && msgs[0].Role == keys.AgentRoleSystem {
		if strings.TrimSpace(msgs[0].ContentBlocksToText()) == p {
			return
		}
		msgs[0] = communi.NewSystemMessageWithoutId(p)
		a.contextController.ReplaceMessages(msgs)
		return
	}
	a.contextController.ReplaceMessages(append([]communi.Message{communi.NewSystemMessageWithoutId(p)}, msgs...))
}

func (a *Agent) ContextFilePath() []string {
	return a.contextController.Paths
}

// CheckPermission provides a shared permission entrypoint for custom resources,
// so callers do not need to depend on tool execution flow.
func (a *Agent) CheckPermission(ctx context.Context, req harness.PermissionRequest) (harness.PermissionResult, error) {
	if a == nil || a.permissionCtrl == nil {
		return harness.PermissionResult{}, nil
	}
	return a.permissionCtrl.Check(ctx, req)
}

// State AgentState holds the mutable state of an Agent instance.
type State struct {
	WorkDir string

	ThinkingLevel keys.ThinkingLevel

	IsStreaming   bool
	StreamMessage *communi.Message
	Error         error
	LastErrorKind errors.ErrKind // classification of Error for UI

	// LastUsage captures the most recent token usage reported by the LLM
	// provider for a completed turn, if available.
	LastUsage *communi.Usage

	// LastStopReason records the last completion stop reason reported by
	// the provider (e.g. "stop", "length", "toolUse", "error", "aborted").
	LastStopReason string
}
