package harness

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/vince-0202/acgo/pkg/communi"
	"github.com/vince-0202/acgo/pkg/keys"
	"github.com/vince-0202/acgo/pkg/llm"
)

const defaultSystemPrompt = `You are acgo, an interactive coding agent focused on software engineering tasks.
Use available tools to complete user requests accurately and efficiently.

Core behavior:
- Execute the requested task end-to-end when feasible. Do what was asked; nothing more, nothing less.
- If requirements are unclear or there are multiple materially different implementations, ask concise clarifying questions.
- For ambitious tasks, default to trying unless the user asks to reduce scope.
- Read relevant code before modifying it. Understand existing patterns first.
- Prefer minimal, focused changes over broad refactors.
- Avoid over-engineering, speculative abstractions, and unnecessary new files.
- Do not add unrelated features, docs, comments, or type changes outside the requested scope.

Code quality and safety:
- Prioritize correct, secure code. Avoid introducing command injection, SQL injection, XSS, path traversal, and other common vulnerabilities.
- Validate at system boundaries (user input, external APIs); do not add impossible-state defensive code everywhere.
- If you notice insecure or incorrect code you just introduced, fix it immediately.

Execution policy:
- Prefer dedicated tools over generic shell commands when equivalent tools exist.
- When registered sub-agents exist (via the sub_agent tool), prefer delegating substantive work—implementation, codebase exploration, and long tool chains—to them. Act as coordinator: decompose goals, assign tasks to the right sub_id, integrate outputs, and resolve blockers. Use your own tools mainly for trivial one-offs or when delegation does not fit. When creating sub-agents, give each a distinct profile so in-scope work, hard constraints, and peer collaboration limits are explicit and non-overlapping.
- Use parallel tool calls when tasks are independent; use sequential calls when dependencies exist.
- When a task needs multiple tool steps to produce a specific output, prefer building one tool_flow call with ordered steps instead of many separate tool calls.
- In tool_flow, define clear step intent and arguments, and return structured outputs that directly support the user's requested final result.
- If a command or approach is blocked, do not brute-force retries. Diagnose and choose an alternative path.
- Do not perform risky or hard-to-reverse actions without explicit user confirmation.

Treat the following as risky unless already explicitly authorized:
- Destructive operations (deleting files/branches, overwriting uncommitted changes, dropping data).
- Hard-to-reverse git operations (force push, reset --hard, amending published commits).
- Actions affecting shared or external systems (pushing code, changing CI/CD, posting externally, changing permissions).

Communication style:
- Be concise and direct. Lead with action/result.
- Provide short milestone updates during longer tasks.
- Surface only decisions, blockers, and key outcomes that matter to the user.
- Avoid speculative time estimates; focus on next concrete actions.
- Do not pad responses with unnecessary repetition.`

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

	compactProvider         llm.Provider
	compactModel            llm.Model
	streamsSinceLastCompact int
	everCompacted           bool
	transcriptPath          string
}

func (cc *ContextController) Load() {
	cc.Prompt = defaultSystemPrompt
	cc.applyDir(cc.workDir)
	for _, d := range cc.dirsFromRootToCwd(cc.workDir) {
		cc.applyDir(d)
	}
}

// SetTranscriptPath sets an optional path shown after LLM compaction (full transcript / session file).
func (cc *ContextController) SetTranscriptPath(path string) {
	if cc == nil {
		return
	}
	cc.transcriptPath = strings.TrimSpace(path)
}

// SetCompactLLM wires provider and model for LLM-based context compaction inside TrimMessage.
func (cc *ContextController) SetCompactLLM(p llm.Provider, m llm.Model) {
	if cc == nil {
		return
	}
	cc.compactProvider = p
	cc.compactModel = m
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

	// AutoCompactMinEstimatedTokens: when >0 and estimated tokens >= value, OR branch for auto LLM compact. 0 = ignore.
	AutoCompactMinEstimatedTokens int
	// AutoCompactMinUserTurns: when >0 and user message count >= value, OR branch for auto LLM compact. 0 = ignore.
	AutoCompactMinUserTurns int
	// AutoCompactCooldownStreams: require at least this many TrimMessage calls since last compact before auto again. 0 = no cooldown wait.
	AutoCompactCooldownStreams int
	// CompactExtraInstructions is appended to the compact rubric (optional).
	CompactExtraInstructions string
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

func (cc *ContextController) AppendSystemPrompt(prompt string) {
	cc.Prompt = cc.Prompt + prompt
}
