package tui

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/vince-0202/acgo/pkg/agent"
	"github.com/vince-0202/acgo/pkg/errors"
	"github.com/vince-0202/acgo/pkg/keys"
	"github.com/vince-0202/acgo/pkg/session"
)

func (m *Model) startAgentStream(prompt string, emit func(streamEvent)) {
	ch := make(chan streamEvent, 64)
	var done atomic.Bool
	go func() {
		m.streamDone = &done
		unsub := m.agent.Subscribe(func(event agent.Event, abort func()) {
			m.syncSubAgentSubscriptions(ch, &done)
			m.handleAgentEvent(event, &done, ch, "")
		})
		defer unsub()
		m.syncSubAgentSubscriptions(ch, &done)

		ctx := context.Background()
		if m.session != nil && strings.TrimSpace(m.session.Path) != "" {
			ctx = session.WithSessionID(ctx, m.session.Path)
		}
		err := m.agent.Prompt(ctx, prompt)
		done.Store(true)
		m.unsubscribeAllSubAgentsForCh(ch)
		m.streamDone = nil
		ch <- streamEvent{Done: true, Err: err, Ch: ch, SubID: ""}
		close(ch)
	}()
	go func() {
		for ev := range ch {
			if emit != nil {
				emit(ev)
			}
		}
	}()
}

func (m *Model) handleAgentEvent(event agent.Event, done *atomic.Bool, ch chan streamEvent, subID string) {
	if done.Load() {
		return
	}
	if subID == "" && m.session != nil && event.Message != nil {
		switch event.Type {
		case agent.EventMessageEnd, agent.EventToolExecutionEnd:
			_ = m.session.AppendMessage(*event.Message)
		}
	}
	switch event.Type {
	case agent.EventMessageUpdate:
		if event.LlmEvent == nil {
			return
		}
		if event.LlmEvent.TextDelta != "" {
			select {
			case ch <- streamEvent{Delta: event.LlmEvent.TextDelta, Ch: ch, SubID: subID}:
			default:
			}
		}
		if event.LlmEvent.ThinkingDelta != "" {
			select {
			case ch <- streamEvent{ThinkingDelta: event.LlmEvent.ThinkingDelta, Ch: ch, SubID: subID}:
			default:
			}
		}
	case agent.EventMessageEnd:
		if event.Message == nil || event.Message.Role != keys.AgentRoleAssistant {
			return
		}
		finalThinking := strings.TrimSpace(event.Message.Thinking)
		finalContent := strings.TrimSpace(event.Message.ContentBlocksToText())
		if finalThinking == "" && finalContent == "" {
			return
		}
		select {
		case ch <- streamEvent{FinalThinking: finalThinking, FinalContent: finalContent, Ch: ch, SubID: subID}:
		default:
		}
	case agent.EventTurnEnd:
		if event.LlmEvent != nil && event.LlmEvent.Usage != nil {
			usage := *event.LlmEvent.Usage
			select {
			case ch <- streamEvent{TurnUsage: &usage, Ch: ch, SubID: subID}:
			default:
			}
		}
	case agent.EventToolExecutionStart:
		toolName := ""
		if event.Tool != nil {
			toolName = strings.TrimSpace(event.Tool.Name())
		}
		if toolName == "" {
			return
		}
		select {
		case ch <- streamEvent{ToolLine: &toolLineStreamEvent{
			Phase:      toolLinePhaseStart,
			ToolCallID: event.ToolCallID,
			ToolName:   toolName,
		}, Ch: ch, SubID: subID}:
		default:
		}
	case agent.EventToolExecutionEnd:
		toolName := ""
		if event.Tool != nil {
			toolName = strings.TrimSpace(event.Tool.Name())
		}
		if toolName == "" {
			return
		}
		select {
		case ch <- streamEvent{ToolLine: &toolLineStreamEvent{
			Phase:      toolLinePhaseEnd,
			ToolCallID: event.ToolCallID,
			ToolName:   toolName,
			Failed:     event.Error != nil || (event.Message != nil && event.Message.IsError),
		}, Ch: ch, SubID: subID}:
		default:
		}
	}
}

func (m *Model) applyToolLine(msg *toolLineStreamEvent) {
	if msg == nil {
		return
	}
	switch msg.Phase {
	case toolLinePhaseStart:
		line := formatToolLine(msg.ToolName, "running")
		m.toolPendingIdx[msg.ToolCallID] = len(m.history)
		m.history = append(m.history, line)
	case toolLinePhaseEnd:
		line := formatToolLine(msg.ToolName, ternary(msg.Failed, "error", "ok"))
		if idx, ok := m.toolPendingIdx[msg.ToolCallID]; ok && idx >= 0 && idx < len(m.history) {
			m.history[idx] = line
			delete(m.toolPendingIdx, msg.ToolCallID)
			return
		}
		m.history = append(m.history, line)
	}
}

func (m *Model) syncSubAgentSubscriptions(ch chan streamEvent, done *atomic.Bool) {
	chKey := fmt.Sprintf("%p", ch)
	for _, info := range m.agent.SubAgentManager().List() {
		subID := info.ID
		key := subID + "|" + chKey
		if _, exists := m.subAgentSubKeys[key]; exists {
			continue
		}
		child, ok := m.agent.SubAgentManager().Get(subID)
		if !ok || child == nil {
			continue
		}
		unsub := child.Subscribe(func(event agent.Event, abort func()) {
			m.handleAgentEvent(event, done, ch, subID)
		})
		m.subAgentSubKeys[key] = unsub
	}
}

func (m *Model) unsubscribeAllSubAgentsForCh(ch chan streamEvent) {
	chKey := fmt.Sprintf("%p", ch)
	for key, unsub := range m.subAgentSubKeys {
		if !strings.HasSuffix(key, "|"+chKey) {
			continue
		}
		if unsub != nil {
			unsub()
		}
		delete(m.subAgentSubKeys, key)
	}
}

func (m *Model) applyStreamEventToSub(msg streamEvent) {
	subID := strings.TrimSpace(msg.SubID)
	if subID == "" {
		return
	}
	p := m.ensureSubPanel(subID)
	if msg.TurnUsage != nil {
		m.accumulateSessionUsage(msg.TurnUsage)
	}
	if msg.FinalThinking != "" {
		p.history = append(p.history, "[Thinking] "+msg.FinalThinking)
		p.streamingThinking = ""
	}
	if msg.FinalContent != "" {
		p.history = append(p.history, "Assistant: "+msg.FinalContent)
		p.streamingContent = ""
	}
	if msg.ToolLine != nil {
		switch msg.ToolLine.Phase {
		case toolLinePhaseStart:
			line := formatToolLine(msg.ToolLine.ToolName, "running")
			p.toolPendingIdx[msg.ToolLine.ToolCallID] = len(p.history)
			p.history = append(p.history, line)
		case toolLinePhaseEnd:
			line := formatToolLine(msg.ToolLine.ToolName, ternary(msg.ToolLine.Failed, "error", "ok"))
			if idx, ok := p.toolPendingIdx[msg.ToolLine.ToolCallID]; ok && idx >= 0 && idx < len(p.history) {
				p.history[idx] = line
				delete(p.toolPendingIdx, msg.ToolLine.ToolCallID)
			} else {
				p.history = append(p.history, line)
			}
		}
	}
	if msg.ThinkingDelta != "" {
		p.streamingThinking += msg.ThinkingDelta
	}
	if msg.Delta != "" {
		p.streamingContent += msg.Delta
	}
	if msg.Done || msg.Err != nil {
		if msg.Err != nil {
			if text := strings.TrimSpace(errors.FormatErrorForDisplay(msg.Err)); text != "" {
				p.history = append(p.history, "Error: "+text)
			}
		} else {
			if p.streamingThinking != "" {
				p.history = append(p.history, "[Thinking] "+p.streamingThinking)
			}
			if p.streamingContent != "" {
				p.history = append(p.history, "Assistant: "+p.streamingContent)
			}
		}
		p.streamingThinking = ""
		p.streamingContent = ""
	}
}
