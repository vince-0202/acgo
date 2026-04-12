package agent

import (
	"context"
	"github.com/vince-0202/acgo/pkg/communi"
	"github.com/vince-0202/acgo/pkg/errors"
	"github.com/vince-0202/acgo/pkg/llm"
	"github.com/vince-0202/acgo/pkg/utils"
)

const planModeInstructionPrompt = "Plan mode is active. Your final response must be a concrete coding plan only (steps, scope, and verification), not code changes. Do not call task-editing tools such as write/edit, and do not perform mutating operations."

type turnManager struct {
	roundNumber int
}

func (tm *turnManager) reset() {
	if tm == nil {
		return
	}
	tm.roundNumber = 0
}

func (tm *turnManager) startAgentTurn(ctx context.Context, agent *Agent, initialMsg *communi.Message) *errors.AgentError {
	for {
		turnMsg := initialMsg
		if !tm.IsFirstTurn() {
			turnMsg = agent.queueManager.DrainOneFromQueues()
		}
		if turnMsg == nil {
			break
		}
		lastTurn := newTurn(agent)
		agent.emit(NewEvent(
			WithEventType(EventTurnStart),
			WithEventAgent(agent),
			WithEventTurnId(lastTurn.id),
		))
		if agentError := lastTurn.run(ctx, turnMsg); agentError != nil {
			return agentError
		}
		tm.roundNumber++
	}
	return nil
}

func (tm *turnManager) IsFirstTurn() bool {
	return tm.roundNumber == 0
}

func newTurn(agent *Agent) *turn {
	return &turn{
		id:    utils.SnowflakeIDString(),
		agent: agent,
	}
}

type turn struct {
	id    string
	agent *Agent
}

func (t *turn) run(ctx context.Context, turnMsg *communi.Message) *errors.AgentError {
	if turnMsg == nil {
		return nil
	}

	t.agent.emitUserMessage(t.id, turnMsg)
	if lastErr := t.runLLMTurnsUntilDone(ctx); lastErr != nil {
		return lastErr
	}
	return nil
}

// runLLMTurnsUntilDone runs stream turns and tool execution until no more tool calls or an error.
func (t *turn) runLLMTurnsUntilDone(ctx context.Context) *errors.AgentError {
	for {
		streamErr, hasToolCalls := t.runOneStream(ctx)
		if streamErr != nil {
			return streamErr
		}
		if !hasToolCalls {
			return nil
		}
		t.agent.toolManager.Execute(ctx, t)
	}
}

// runOneStream performs one LLM stream call: build context, stream, process events, append assistant, emit done.
// Returns (error if any, whether there are pending tool calls to execute).
func (t *turn) runOneStream(ctx context.Context) (err *errors.AgentError, hasToolCalls bool) {
	ctx, cancel := context.WithCancel(ctx)
	t.agent.state.IsStreaming = true
	t.agent.currentCancel = cancel
	defer func() {
		t.agent.state.IsStreaming = false
		t.agent.currentCancel = nil
	}()

	t.agent.toolManager.ClearPendingTool()

	opts := &llm.Options{
		Tools:           t.agent.toolManager.GetToolSchemas(),
		ReasoningEffort: t.agent.state.ThinkingLevel,
	}
	llmMessages := t.agent.context.Messages
	if t.agent.planMode {
		llmMessages = append([]communi.Message(nil), llmMessages...)
		llmMessages = append(llmMessages, communi.NewSystemMessageWithoutId(planModeInstructionPrompt))
	}
	t.agent.emit(NewEvent(
		WithEventType(EventBeforeLLMCall),
		WithEventAgent(t.agent),
		WithEventTurnId(t.id),
	))

	events, streamErr := t.agent.provider.Stream(ctx, t.agent.model, llmMessages, opts)
	if streamErr != nil {
		agentErr := errors.WrapError(streamErr)
		t.agent.state.Error = agentErr
		t.agent.emit(NewEvent(
			WithEventType(EventTurnEnd),
			WithEventAgent(t.agent),
			WithEventTurnId(t.id),
			WithEventError(agentErr),
		))
		return agentErr, false
	}

	assistant := communi.NewAssistantMessage(t.id, "")
	t.agent.state.StreamMessage = &assistant
	t.agent.emit(NewEvent(
		WithEventType(EventMessageStart),
		WithEventAgent(t.agent),
		WithEventTurnId(t.id),
		WithEventMessage(&assistant),
	))
	lastDone := t.processStreamEvents(events, &assistant)

	t.agent.state.StreamMessage = nil
	pendingToolCalls := t.agent.toolManager.GetPendingToolCalls()
	if len(pendingToolCalls) > 0 {
		assistant.ToolCalls = append([]communi.ToolCallRequest(nil), pendingToolCalls...)
		assistant.ToolCall = &assistant.ToolCalls[0]
	}
	t.agent.context.AppendMessage(assistant)

	if lastDone != nil {
		t.agent.state.LastStopReason = lastDone.StopReason
		if lastDone.Usage != nil {
			t.agent.state.LastUsage = lastDone.Usage
		} else {
			t.agent.state.LastUsage = nil
		}
	}
	t.agent.emit(NewEvent(
		WithEventType(EventAfterLLMCall),
		WithEventAgent(t.agent),
		WithEventTurnId(t.id),
		WithEventMessage(&assistant),
		WithEventLLMEvent(lastDone),
	))
	t.agent.emit(NewEvent(
		WithEventType(EventMessageEnd),
		WithEventAgent(t.agent),
		WithEventTurnId(t.id),
		WithEventMessage(&assistant),
	))

	t.agent.emit(NewEvent(
		WithEventType(EventTurnEnd),
		WithEventAgent(t.agent),
		WithEventTurnId(t.id),
		WithEventMessage(&assistant),
		WithEventLLMEvent(lastDone),
	))
	return nil, len(pendingToolCalls) > 0
}

// processStreamEvents consumes the event channel and updates assistant state; emits MessageUpdate events.
func (t *turn) processStreamEvents(events <-chan communi.LLMEvent, assistant *communi.Message) (lastDone *communi.LLMEvent) {
	for ev := range events {
		switch ev.Type {
		case communi.EventTextDelta:
			assistant.AppendTextValue(ev.TextDelta)
			t.agent.emit(NewEvent(
				WithEventType(EventMessageUpdate),
				WithEventAgent(t.agent),
				WithEventTurnId(t.id),
				WithEventMessage(assistant),
				WithEventLLMEvent(&ev),
			))
		case communi.EventThinkingStart:
			evCopy := ev
			t.agent.emit(NewEvent(
				WithEventType(EventMessageUpdate),
				WithEventAgent(t.agent),
				WithEventTurnId(t.id),
				WithEventMessage(assistant),
				WithEventLLMEvent(&evCopy),
			))
		case communi.EventThinkingDelta:
			assistant.Thinking += ev.ThinkingDelta
			evCopy := ev
			t.agent.emit(NewEvent(
				WithEventType(EventMessageUpdate),
				WithEventAgent(t.agent),
				WithEventTurnId(t.id),
				WithEventMessage(assistant),
				WithEventLLMEvent(&evCopy),
			))
		case communi.EventThinkingEnd:
			evCopy := ev
			t.agent.emit(NewEvent(
				WithEventType(EventMessageUpdate),
				WithEventAgent(t.agent),
				WithEventTurnId(t.id),
				WithEventMessage(assistant),
				WithEventLLMEvent(&evCopy),
			))
		case communi.EventToolCallStart, communi.EventToolCallDelta, communi.EventToolCallEnd:
			if ev.ToolCall != nil {
				t.agent.toolManager.UpsertPendingToolCall(*ev.ToolCall)
			}
		case communi.EventDone:
			evCopy := ev
			lastDone = &evCopy
		}
	}
	return
}
