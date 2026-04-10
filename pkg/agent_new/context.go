package agent_new

import (
	"context"
	"github.com/vince-0202/acgo/pkg/communi"
)

const defaultSystemPrompt = "You are a helpful coding assistant.Your answer needs to be accurate and concise."

type ContextBuildOption func(*Context)

// Context contains the full list of messages for a call.
// It can be freely manipulated before being passed to a provider.
type Context struct {
	workDir  string
	Messages []communi.Message `json:"messages"`
	Options  *ContextOptions
	Cancel   context.CancelFunc

	// projectRoot, when set, overrides workDir for Load() (SYSTEM.md / AGENTS.md walk) and tool working directory.
	projectRoot string
	Paths       []string // File paths that were read (for logging/debug)
	Prompt      string   // Final merged system prompt
}

func (c *Context) ClearMessages() {
	c.Messages = nil
}

func (c *Context) AppendMessage(msg ...communi.Message) {
	if c.Messages == nil {
		c.Messages = make([]communi.Message, 0)
	}
	c.Messages = append(c.Messages, msg...)
}

// ReplaceMessages replaces the entire message history with a copy of msgs.
// Callers can use this to load a session or batch-replace history.
func (c *Context) ReplaceMessages(msgs []communi.Message) {
	if msgs == nil {
		c.Messages = nil
		return
	}
	c.Messages = append([]communi.Message(nil), msgs...)
}

// ContextOptions agent context
type ContextOptions struct {
	// MaxTurns is the maximum number of conversation turns to keep (each turn = one user message + assistant/tool replies). 0 = no limit.
	MaxTurns int
	// MaxEstimatedTokens is a soft cap on total estimated tokens (rough ~4 chars/token). 0 = disabled.
	MaxEstimatedTokens int
	// MinToolResultsToKeep is the minimum number of recent tool-result messages to always retain.
	MinToolResultsToKeep int

	// AutoCompactMinEstimatedTokens: when >0 and estimated tokens >= value, OR branch for auto LLM compact. 0 = ignore.
	AutoCompactMinEstimatedTokens int
	// AutoCompactMinUserTurns: when >0 and user message count >= value, OR branch for auto LLM compact. 0 = ignore.
	AutoCompactMinUserTurns int
	// AutoCompactCooldownStreams: require at least this many TrimMessage calls since last compact before auto again. 0 = no cooldown wait.
	AutoCompactCooldownStreams int
	// CompactExtraInstructions is appended to the compact rubric (optional).
	CompactExtraInstructions string
}
