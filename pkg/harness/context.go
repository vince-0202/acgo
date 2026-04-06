package harness

import (
	"context"
	"encoding/json"
	"github.com/vince-0202/acgo/pkg/communi"
	"github.com/vince-0202/acgo/pkg/keys"
	"os"
	"path/filepath"
	"strings"
)

const defaultSystemPrompt = "You are a helpful coding assistant.Your answer needs to be accurate and concise."

func NewContextController(agentId, workDir string) *ContextController {
	return &ContextController{
		agentId:  agentId,
		workDir:  filepath.Join(workDir, agentId),
		Options:  &ContextOptions{},
		Messages: make([]communi.Message, 0),
		Paths:    make([]string, 0),
		Prompt:   defaultSystemPrompt,
	}
}

// ContextController contains the full list of messages for a call.
// It can be freely manipulated before being passed to a provider.
type ContextController struct {
	agentId  string
	workDir  string
	Paths    []string // File paths that were read (for logging/debug)
	Prompt   string   // Final merged system prompt
	Options  *ContextOptions
	Messages []communi.Message `json:"messages"`
	Cancel   context.CancelFunc
}

func (cc *ContextController) Load() {
	cc.Prompt = defaultSystemPrompt
	cc.applyDir(cc.workDir)
	for _, d := range cc.dirsFromRootToCwd(cc.workDir) {
		cc.applyDir(d)
	}
}

// TrimMessage applies the trimming strategy defined by opts.
func (cc *ContextController) TrimMessage() {
	//todo:
}

func (cc *ContextController) MessageToJson() string {
	res, _ := json.Marshal(cc.Messages)
	return string(res)
}

func (cc *ContextController) AppendMessage(msg ...communi.Message) {
	if cc.Messages == nil {
		cc.Messages = make([]communi.Message, 0)
	}
	cc.Messages = append(cc.Messages, msg...)
}

func (cc *ContextController) LastAssistantMessage() *communi.Message {
	for _, message := range cc.Messages {
		if message.Role == keys.AgentRoleAssistant {
			return &message
		}
	}
	return nil
}

// ReplaceMessages replaces the entire message history with a copy of msgs.
// Callers can use this to load a session or batch-replace history.
func (cc *ContextController) ReplaceMessages(msgs []communi.Message) {
	if msgs == nil {
		cc.Messages = nil
		return
	}
	cc.Messages = append([]communi.Message(nil), msgs...)
}

func (cc *ContextController) ClearMessages() {
	cc.Messages = nil
}

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

func readFile(dir, name string) (content string, path string) {
	path = filepath.Join(dir, name)
	b, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	return string(b), path
}

// applyDir reads SYSTEM.md (replaces), AGENTS.md and APPEND_SYSTEM.md (append) from dir.
func (cc *ContextController) applyDir(dir string) {
	// SYSTEM.md: replace
	if b, p := readFile(dir, "SYSTEM.md"); b != "" {
		cc.Prompt = strings.TrimSpace(b)
		cc.Paths = append(cc.Paths, p)
	}
	// AGENTS.md: append
	if b, p := readFile(dir, "AGENTS.md"); b != "" {
		cc.Prompt = cc.Prompt + "\n\n" + strings.TrimSpace(b)
		cc.Paths = append(cc.Paths, p)
	}
	// APPEND_SYSTEM.md: append
	if b, p := readFile(dir, "APPEND_SYSTEM.md"); b != "" {
		cc.Prompt = cc.Prompt + "\n\n" + strings.TrimSpace(b)
		cc.Paths = append(cc.Paths, p)
	}
}

// dirsFromRootToCwd returns directories from filesystem root toward workDir
// (e.g. ["/", "/home", "/home/user", "/home/user/proj"] so workDir wins when we apply in order).
func (cc *ContextController) dirsFromRootToCwd(workDir string) []string {
	abs, err := filepath.Abs(workDir)
	if err != nil || abs == "" {
		return nil
	}
	abs = filepath.Clean(abs)
	var parts []string
	for {
		parts = append(parts, abs)
		parent := filepath.Dir(abs)
		if parent == abs {
			break
		}
		abs = parent
	}
	// parts is [workDir, parent, ..., root]; reverse to get root ... workDir
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return parts
}
