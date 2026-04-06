package harness

import (
	"context"
	"encoding/json"
	"github.com/vince-0202/acgo/pkg/communi"
	"github.com/vince-0202/acgo/pkg/keys"
)

// ContextOptions configures context trimming. Used by defaultTransformContext
// and can be extended later (e.g. SummarizeFunc for summarizing dropped messages).
type ContextOptions struct {
	// MaxTurns is the maximum number of conversation turns to keep (each turn = one user message + assistant/tool replies). 0 = no limit.
	MaxTurns int
	// MaxEstimatedTokens is a soft cap on total estimated tokens (rough ~4 chars/token). 0 = disabled.
	MaxEstimatedTokens int
	// MinToolResultsToKeep is the minimum number of recent tool-result messages to always retain.
	MinToolResultsToKeep int
}

func NewContextController(agentId string) *ContextController {
	return &ContextController{
		agentId:  agentId,
		Options:  &ContextOptions{},
		Messages: make([]communi.Message, 0),
	}
}

// ContextController contains the full list of messages for a call.
// It can be freely manipulated before being passed to a provider.
type ContextController struct {
	agentId  string
	Options  *ContextOptions
	Messages []communi.Message `json:"messages"`
	Cancel   context.CancelFunc
}

// TrimMessage applies the trimming strategy defined by opts.
func (c ContextController) TrimMessage() {
	//todo:
}

func (c ContextController) MessageToJson() string {
	res, _ := json.Marshal(c.Messages)
	return string(res)
}

func (c *ContextController) AppendMessage(msg ...communi.Message) {
	if c.Messages == nil {
		c.Messages = make([]communi.Message, 0)
	}
	c.Messages = append(c.Messages, msg...)
}

func (c ContextController) LastAssistantMessage() *communi.Message {
	for _, message := range c.Messages {
		if message.Role == keys.AgentRoleAssistant {
			return &message
		}
	}
	return nil
}

// ReplaceMessages replaces the entire message history with a copy of msgs.
// Callers can use this to load a session or batch-replace history.
func (a *ContextController) ReplaceMessages(msgs []communi.Message) {
	if msgs == nil {
		a.Messages = nil
		return
	}
	a.Messages = append([]communi.Message(nil), msgs...)
}

func (c *ContextController) ClearMessages() {
	c.Messages = nil
}
