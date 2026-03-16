package agent

import (
	"context"

	"acgo/pkg/llm"
)

// TransformContextOptions configures context trimming. Used by defaultTransformContext
// and can be extended later (e.g. SummarizeFunc for summarizing dropped messages).
type TransformContextOptions struct {
	// MaxTurns is the maximum number of conversation turns to keep (each turn = one user message + assistant/tool replies). 0 = no limit.
	MaxTurns int
	// MaxEstimatedTokens is a soft cap on total estimated tokens (rough ~4 chars/token). 0 = disabled.
	MaxEstimatedTokens int
	// MinToolResultsToKeep is the minimum number of recent tool-result messages to always retain.
	MinToolResultsToKeep int
}

// DefaultTransformContextOptions returns the default trimming limits.
func DefaultTransformContextOptions() TransformContextOptions {
	return TransformContextOptions{
		MaxTurns:             20,
		MaxEstimatedTokens:   0, // disabled by default
		MinToolResultsToKeep: 10,
	}
}

// AgentMessage is the application-facing message type.
type AgentMessage struct {
	ID         string           // unique identifier within a session
	Role       AgentMessageRole // logical role
	Content    string           // rendered text content (for UI)
	Thinking   string           // reasoning/thinking stream from models that support it (e.g. DeepSeek R1, o1)
	ToolCallID string           // when Role is RoleTool, required for OpenAI-style APIs
	LlmMessage *llm.Message     // backing LLM message when applicable
	IsError    bool             // whether this message represents an error
	Metadata   map[string]any   // arbitrary metadata
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
			if m.Role == RoleAssistant {
				msg.Thinking = m.Thinking
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

// estimateMessageTokens returns a rough token count for the message (content + thinking).
// Uses ~4 characters per token as a simple approximation for mixed languages.
func estimateMessageTokens(m AgentMessage) int {
	n := len(m.Content) + len(m.Thinking)
	if m.LlmMessage != nil {
		for _, b := range m.LlmMessage.Content {
			n += len(b.Text)
		}
		n += len(m.LlmMessage.Thinking)
	}
	return (n + 3) / 4
}

// defaultTransformContext trims the message list to fit within turn/token limits while
// always keeping leading system messages and a minimum number of recent tool results.
func defaultTransformContext(msgs []AgentMessage, _ context.Context) []AgentMessage {
	return transformContextWithOptions(msgs, DefaultTransformContextOptions())
}

// transformContextWithOptions applies the trimming strategy defined by opts.
func transformContextWithOptions(msgs []AgentMessage, opts TransformContextOptions) []AgentMessage {
	if len(msgs) == 0 {
		return nil
	}
	// Split: leading system messages, then conversation.
	var systemEnd int
	for systemEnd < len(msgs) && msgs[systemEnd].Role == RoleSystem {
		systemEnd++
	}
	system := msgs[:systemEnd]
	conv := msgs[systemEnd:]
	if len(conv) == 0 {
		return system
	}

	// Find start index for "last N turns". A turn starts at a user message.
	startByTurns := startOfLastNTurns(conv, opts.MaxTurns)
	// Find start index so we keep at least the last K tool results.
	startByTools := startToKeepLastKToolResults(conv, opts.MinToolResultsToKeep)
	// Keep the earlier of the two so we satisfy both: keep last N turns AND last K tool results.
	start := min(startByTurns, startByTools)

	trimmed := make([]AgentMessage, 0, len(system)+len(conv)-start)
	trimmed = append(trimmed, system...)
	trimmed = append(trimmed, conv[start:]...)

	// Optional: cap by estimated total tokens.
	if opts.MaxEstimatedTokens > 0 {
		trimmed = trimToMaxEstimatedTokens(trimmed, systemEnd, opts.MaxEstimatedTokens)
	}
	return trimmed
}

// startOfLastNTurns returns the start index in conv so that we keep the last n turns.
// A turn = one user message plus all following messages until the next user message.
// n <= 0 means keep all (return 0).
func startOfLastNTurns(conv []AgentMessage, n int) int {
	if n <= 0 {
		return 0
	}
	// Count user messages from the end; the n-th user message (from end) starts a turn we must keep.
	userCount := 0
	for i := len(conv) - 1; i >= 0; i-- {
		if conv[i].Role == RoleUser {
			userCount++
			if userCount == n {
				return i
			}
		}
	}
	return 0
}

// startToKeepLastKToolResults returns the start index so that the last K tool messages are kept.
// Returns len(conv) when k <= 0 so min(with startOfLastNTurns) is not constrained by tools.
func startToKeepLastKToolResults(conv []AgentMessage, k int) int {
	if k <= 0 {
		return len(conv)
	}
	toolCount := 0
	for i := len(conv) - 1; i >= 0; i-- {
		if conv[i].Role == RoleTool {
			toolCount++
			if toolCount == k {
				return i
			}
		}
	}
	return 0
}

// trimToMaxEstimatedTokens shrinks the conversation part (after system) so total estimated tokens <= max.
// System messages are always kept. Trimming advances to the next user message so we don't cut mid-turn.
func trimToMaxEstimatedTokens(msgs []AgentMessage, systemLen int, maxTokens int) []AgentMessage {
	if systemLen >= len(msgs) || maxTokens <= 0 {
		return msgs
	}
	systemTokens := 0
	for i := 0; i < systemLen; i++ {
		systemTokens += estimateMessageTokens(msgs[i])
	}
	if systemTokens >= maxTokens {
		return msgs[:systemLen]
	}
	conv := msgs[systemLen:]
	total := systemTokens
	for _, m := range conv {
		total += estimateMessageTokens(m)
	}
	if total <= maxTokens {
		return msgs
	}
	trim := 0
	for trim < len(conv) && total > maxTokens {
		total -= estimateMessageTokens(conv[trim])
		trim++
	}
	for trim < len(conv) && conv[trim].Role != RoleUser {
		trim++
	}
	out := make([]AgentMessage, 0, systemLen+len(conv)-trim)
	out = append(out, msgs[:systemLen]...)
	out = append(out, conv[trim:]...)
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
