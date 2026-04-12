package tui

import (
	"strings"

	"github.com/vince-0202/acgo/pkg/communi"
	"github.com/vince-0202/acgo/pkg/keys"
)

func splitLeadingSystem(msgs []communi.Message) (prefix []communi.Message, body []communi.Message) {
	i := 0
	for i < len(msgs) && msgs[i].Role == keys.AgentRoleSystem {
		i++
	}
	return append([]communi.Message(nil), msgs[:i]...), append([]communi.Message(nil), msgs[i:]...)
}

func renderHistoryFromMessages(msgs []communi.Message) []string {
	if len(msgs) == 0 {
		return nil
	}
	out := make([]string, 0, len(msgs))
	for _, msg := range msgs {
		lines := renderHistoryLinesFromMessage(msg)
		if len(lines) == 0 {
			continue
		}
		out = append(out, lines...)
	}
	return out
}

func renderHistoryLinesFromMessage(msg communi.Message) []string {
	content := strings.TrimSpace(msg.ContentBlocksToText())
	thinking := strings.TrimSpace(msg.Thinking)
	switch msg.Role {
	case keys.AgentRoleUser:
		if content == "" {
			return nil
		}
		return []string{"You: " + content}
	case keys.AgentRoleAssistant:
		lines := make([]string, 0, 2)
		if thinking != "" {
			lines = append(lines, "[Thinking] "+thinking)
		}
		if content != "" {
			lines = append(lines, "Assistant: "+content)
		}
		return lines
	case keys.AgentRoleTool:
		name := ""
		var args []byte
		if msg.ToolCall != nil {
			name = strings.TrimSpace(msg.ToolCall.Name)
			args = msg.ToolCall.Arguments
		}
		if name != "" {
			status := "ok"
			if msg.IsError {
				status = "error"
			}
			return []string{formatToolLineWithArgs(name, args, status)}
		}
		if content == "" {
			return nil
		}
		return []string{"Tool: " + content}
	case keys.AgentRoleSystem:
		if content == "" {
			return nil
		}
		return []string{"System: " + content}
	case keys.AgentRoleNotification:
		return nil
	default:
		if content == "" {
			return nil
		}
		return []string{string(msg.Role) + ": " + content}
	}
}
