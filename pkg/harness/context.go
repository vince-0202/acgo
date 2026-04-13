package harness

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/vince-0202/acgo/pkg/agent"
	"github.com/vince-0202/acgo/pkg/communi"
	"github.com/vince-0202/acgo/pkg/keys"
)

// TrimMode selects how TrimMessage behaves.
type TrimMode int

const (
	TrimModeDeterministic TrimMode = iota
	TrimModeLLMCompact
)

// TrimMessageOptions configures a single TrimMessage invocation.
type TrimMessageOptions struct {
	Mode TrimMode
	// Force skips auto OR/cooldown checks when Mode is TrimModeLLMCompact (manual /compact).
	Force             bool
	TranscriptPath    string
	ExtraInstructions string
}

type ContextController struct {
	agent   agent.AgentRuntime
	context agent.ContextRuntime

	basePrompt        string
	persistentAppends []string

	streamsSinceLastCompact int
	everCompacted           bool
	transcriptPath          string
}

func NewContextController() *ContextController {
	return &ContextController{}
}

func (cc *ContextController) Name() string {
	return "context"
}

func (cc *ContextController) Install(runtime agent.AgentRuntime) (func(), error) {
	cc.agent = runtime
	cc.context = runtime.ContextManager()
	cc.basePrompt = cc.context.SystemPrompt()
	cc.loadPromptFromDisk()
	unsub := runtime.Subscribe(func(event agent.Event, abort func()) {
		switch event.Type {
		case agent.EventAgentStart:
			cc.loadPromptFromDisk()
		case agent.EventBeforeLLMCall:
			_ = cc.TrimMessage(context.Background(), nil)
		}
	})
	return func() {
		unsub()
		cc.agent = nil
		cc.context = nil
	}, nil
}

// TrimMessage applies context reduction. Pass nil req for automatic mode: may run LLM compact when
// ContextOptions triggers match and compact LLM is configured; otherwise deterministic trim only.
func (cc *ContextController) TrimMessage(ctx context.Context, req *TrimMessageOptions) error {
	if cc == nil || cc.context == nil {
		return nil
	}
	opts := cc.context.ContextOptions()
	if opts == nil {
		return nil
	}

	switch {
	case req != nil && req.Mode == TrimModeDeterministic:
		cc.applyDeterministicTrim(opts)
		cc.noteStreamAfterTrim(false)
		return nil

	case req != nil && req.Mode == TrimModeLLMCompact && req.Force:
		err := cc.compactWithLLM(ctx, req)
		if err != nil {
			return err
		}
		cc.noteStreamAfterTrim(true)
		return nil

	case req == nil:
		if cc.shouldAutoCompact(opts) {
			treq := &TrimMessageOptions{
				Mode:              TrimModeLLMCompact,
				Force:             false,
				TranscriptPath:    cc.transcriptPath,
				ExtraInstructions: strings.TrimSpace(opts.CompactExtraInstructions),
			}
			err := cc.compactWithLLM(ctx, treq)
			if err != nil {
				return err
			}
			cc.noteStreamAfterTrim(true)
			return nil
		}
		cc.applyDeterministicTrim(opts)
		cc.noteStreamAfterTrim(false)
		return nil

	default:
		cc.applyDeterministicTrim(opts)
		cc.noteStreamAfterTrim(false)
		return nil
	}
}

func (cc *ContextController) shouldAutoCompact(opts *agent.ContextOptions) bool {
	if !cc.hasCompactLLM() {
		return false
	}
	if opts.AutoCompactMinEstimatedTokens <= 0 && opts.AutoCompactMinUserTurns <= 0 {
		return false
	}
	prefix, body := splitLeadingSystem(cc.context.MessageSnapshot())
	_ = prefix
	tok := estimateTotalTokens(body)
	userTurns := countUserMessages(body)

	tokenHit := opts.AutoCompactMinEstimatedTokens > 0 && tok >= opts.AutoCompactMinEstimatedTokens
	turnHit := opts.AutoCompactMinUserTurns > 0 && userTurns >= opts.AutoCompactMinUserTurns
	if !tokenHit && !turnHit {
		return false
	}
	// Cooldown only applies after at least one prior compact (no "previous" compact on first auto).
	if opts.AutoCompactCooldownStreams > 0 && cc.everCompacted && cc.streamsSinceLastCompact < opts.AutoCompactCooldownStreams {
		return false
	}
	return true
}

func (cc *ContextController) noteStreamAfterTrim(didCompact bool) {
	if cc == nil {
		return
	}
	if didCompact {
		cc.everCompacted = true
		cc.streamsSinceLastCompact = 0
		return
	}
	cc.streamsSinceLastCompact++
}

func (cc *ContextController) applyDeterministicTrim(opts *agent.ContextOptions) {
	msgs := cc.context.MessageSnapshot()
	if len(msgs) == 0 {
		return
	}
	if opts.MaxTurns <= 0 && opts.MaxEstimatedTokens <= 0 && opts.MinToolResultsToKeep <= 0 {
		return
	}

	prefix, body := splitLeadingSystem(msgs)
	if len(body) == 0 {
		cc.context.ReplaceMessages(prefix)
		return
	}

	turns := segmentTurnsByUser(body)
	if len(turns) == 0 {
		cc.context.ReplaceMessages(append(append([]communi.Message(nil), prefix...), body...))
		return
	}

	kept := turns
	if opts.MaxTurns > 0 && len(turns) > opts.MaxTurns {
		kept = turns[len(turns)-opts.MaxTurns:]
	}

	kept = ensureMinToolResults(kept, turns, opts.MinToolResultsToKeep)
	out := concatTurns(kept)
	out = trimByTokenBudget(prefix, out, opts.MaxEstimatedTokens)

	cc.context.ReplaceMessages(append(append([]communi.Message(nil), prefix...), out...))
}

func (cc *ContextController) loadPromptFromDisk() {
	if cc == nil || cc.context == nil {
		return
	}
	prompt := strings.TrimSpace(cc.basePrompt)
	if prompt == "" {
		prompt = strings.TrimSpace(cc.context.SystemPrompt())
	}
	var paths []string
	root := cc.effectiveContextRoot()
	absRoot, err := filepath.Abs(root)
	if err != nil {
		absRoot = root
	}
	prompt, paths = cc.applyDir(prompt, paths, absRoot)
	for _, dir := range dirsFromRootToCwd(absRoot) {
		prompt, paths = cc.applyDir(prompt, paths, dir)
	}
	for _, extra := range cc.persistentAppends {
		extra = strings.TrimSpace(extra)
		if extra == "" {
			continue
		}
		if strings.TrimSpace(prompt) == "" {
			prompt = extra
		} else {
			prompt += "\n\n" + extra
		}
	}
	for _, extra := range cc.context.PersistentPrompts() {
		extra = strings.TrimSpace(extra)
		if extra == "" {
			continue
		}
		if strings.TrimSpace(prompt) == "" {
			prompt = extra
		} else {
			prompt += "\n\n" + extra
		}
	}
	cc.context.ReplacePrompt(prompt)
	cc.context.SetLoadedPaths(paths)
}

func (cc *ContextController) AppendPersistentPrompt(prompt string) {
	if cc == nil {
		return
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return
	}
	cc.persistentAppends = append(cc.persistentAppends, prompt)
	if cc.context != nil {
		cc.loadPromptFromDisk()
	}
}

func (cc *ContextController) effectiveContextRoot() string {
	if cc == nil || cc.context == nil {
		return "."
	}
	if s := strings.TrimSpace(cc.context.ProjectRoot()); s != "" {
		return filepath.Clean(s)
	}
	return cc.context.WorkDir()
}

func (cc *ContextController) applyDir(prompt string, paths []string, dir string) (string, []string) {
	if b, p := readFile(dir, "SYSTEM.md"); b != "" {
		prompt = strings.TrimSpace(b)
		paths = append(paths, p)
	}
	if b, p := readFile(dir, "AGENTS.md"); b != "" {
		if strings.TrimSpace(prompt) == "" {
			prompt = strings.TrimSpace(b)
		} else {
			prompt += "\n\n" + strings.TrimSpace(b)
		}
		paths = append(paths, p)
	}
	if b, p := readFile(dir, "APPEND_SYSTEM.md"); b != "" {
		if strings.TrimSpace(prompt) == "" {
			prompt = strings.TrimSpace(b)
		} else {
			prompt += "\n\n" + strings.TrimSpace(b)
		}
		paths = append(paths, p)
	}
	return prompt, paths
}

func readFile(dir, name string) (content string, path string) {
	path = filepath.Join(dir, name)
	b, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	return string(b), path
}

func dirsFromRootToCwd(workDir string) []string {
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
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return parts
}

func splitLeadingSystem(msgs []communi.Message) (prefix []communi.Message, body []communi.Message) {
	i := 0
	for i < len(msgs) && msgs[i].Role == keys.AgentRoleSystem {
		i++
	}
	return append([]communi.Message(nil), msgs[:i]...), append([]communi.Message(nil), msgs[i:]...)
}

func segmentTurnsByUser(body []communi.Message) [][]communi.Message {
	if len(body) == 0 {
		return nil
	}
	var idx []int
	for i := range body {
		if body[i].Role == keys.AgentRoleUser {
			idx = append(idx, i)
		}
	}
	if len(idx) == 0 {
		return [][]communi.Message{append([]communi.Message(nil), body...)}
	}
	var turns [][]communi.Message
	for t := range idx {
		start := idx[t]
		end := len(body)
		if t+1 < len(idx) {
			end = idx[t+1]
		}
		turns = append(turns, append([]communi.Message(nil), body[start:end]...))
	}
	return turns
}

func ensureMinToolResults(kept [][]communi.Message, allTurns [][]communi.Message, min int) [][]communi.Message {
	if min <= 0 {
		return kept
	}
	if len(kept) >= len(allTurns) {
		return kept
	}
	start := len(allTurns) - len(kept)
	if start < 0 {
		start = 0
	}
	for start > 0 && countToolMessages(concatTurns(allTurns[start:])) < min {
		start--
	}
	return allTurns[start:]
}

func countToolMessages(msgs []communi.Message) int {
	n := 0
	for _, m := range msgs {
		if m.Role == keys.AgentRoleTool {
			n++
		}
	}
	return n
}

func concatTurns(turns [][]communi.Message) []communi.Message {
	var out []communi.Message
	for _, t := range turns {
		out = append(out, t...)
	}
	return out
}

func trimByTokenBudget(prefix, body []communi.Message, maxTok int) []communi.Message {
	if maxTok <= 0 {
		return body
	}
	turns := segmentTurnsByUser(body)
	if len(turns) == 0 {
		return body
	}
	kept := turns
	for len(kept) > 1 && estimateTotalTokens(prefix)+estimateTotalTokens(concatTurns(kept)) > maxTok {
		kept = kept[1:]
	}
	return concatTurns(kept)
}

func estimateTotalTokens(msgs []communi.Message) int {
	t := 0
	for _, m := range msgs {
		t += estimateMessageTokens(m)
	}
	return t
}

func estimateMessageTokens(m communi.Message) int {
	chars := len(m.Thinking)
	chars += len(m.ContentBlocksToText())
	if m.ToolCall != nil {
		if b, err := json.Marshal(m.ToolCall); err == nil {
			chars += len(b)
		}
	}
	for _, tc := range m.ToolCalls {
		if b, err := json.Marshal(tc); err == nil {
			chars += len(b)
		}
	}
	if m.ToolResult != nil {
		if b, err := json.Marshal(m.ToolResult); err == nil {
			chars += len(b)
		}
	}
	if chars == 0 {
		return 1
	}
	return (chars + 3) / 4
}

func countUserMessages(msgs []communi.Message) int {
	n := 0
	for _, m := range msgs {
		if m.Role == keys.AgentRoleUser {
			n++
		}
	}
	return n
}
