package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/vince-0202/acgo/pkg/agent"
	"github.com/vince-0202/acgo/pkg/communi"
	"github.com/vince-0202/acgo/pkg/errors"
	"github.com/vince-0202/acgo/pkg/llm"
	"github.com/vince-0202/acgo/pkg/runtime"
	"github.com/vince-0202/acgo/pkg/session"
)

func (m *Model) statusLine() string {
	st := m.agent.State()
	parts := []string{
		providerName(m.agent.Provider()) + "/" + m.agent.Model().ID,
	}
	if m.session != nil && strings.TrimSpace(m.session.Path) != "" {
		parts = append(parts, "session:"+m.session.Path)
	}
	if st.IsStreaming {
		parts = append(parts, "streaming")
	} else {
		parts = append(parts, "idle")
	}
	if n := len(m.agent.SubAgentManager().List()); n > 0 {
		parts = append(parts, fmt.Sprintf("sub:%d", n))
	}
	if st.Error != nil {
		parts = append(parts, errors.FormatErrorForDisplay(st.Error))
	}
	if m.err != nil {
		parts = append(parts, errors.FormatErrorForDisplay(m.err))
	}
	return strings.Join(parts, " | ")
}

func (m *Model) accumulateSessionUsage(u *communi.Usage) {
	if u == nil {
		return
	}
	m.usageIn += u.InputTokens
	m.usageOut += u.OutputTokens
	if u.TotalTokens > 0 {
		m.usageTotal += u.TotalTokens
		return
	}
	m.usageTotal += u.InputTokens + u.OutputTokens
}

func (m *Model) tokenSummaryLine() string {
	return fmt.Sprintf("in: %d · out: %d · total: %d", m.usageIn, m.usageOut, m.usageTotal)
}

func (m *Model) registerCommand(spec commandSpec) {
	name := strings.ToLower(strings.TrimSpace(spec.Name))
	if name == "" || spec.Handle == nil {
		return
	}
	spec.Name = name
	m.commandSpecs = append(m.commandSpecs, spec)
	m.commands[name] = spec
	for _, alias := range spec.Aliases {
		alias = strings.ToLower(strings.TrimSpace(alias))
		if alias == "" {
			continue
		}
		m.commands[alias] = spec
	}
}

func (m *Model) registerBuiltinCommands() {
	m.registerCommand(commandSpec{
		Name:    "help",
		Aliases: []string{"h", "?"},
		Usage:   "/help",
		Help:    "Show available commands.",
		Handle: func(m *Model, _ string) (string, bool) {
			var b strings.Builder
			b.WriteString("commands:\n")
			for _, spec := range m.commandSpecs {
				usage := spec.Usage
				if strings.TrimSpace(usage) == "" {
					usage = "/" + spec.Name
				}
				b.WriteString("  " + usage)
				if strings.TrimSpace(spec.Help) != "" {
					b.WriteString(" - " + strings.TrimSpace(spec.Help))
				}
				b.WriteString("\n")
			}
			return strings.TrimSuffix(b.String(), "\n"), false
		},
	})
	m.registerCommand(commandSpec{
		Name:    "quit",
		Aliases: []string{"q"},
		Usage:   "/quit",
		Help:    "Quit the TUI.",
		Handle:  func(_ *Model, _ string) (string, bool) { return "", true },
	})
	m.registerCommand(commandSpec{
		Name:  "status",
		Usage: "/status",
		Help:  "Show current runtime status.",
		Handle: func(m *Model, _ string) (string, bool) {
			return m.statusLine(), false
		},
	})
	m.registerCommand(commandSpec{
		Name:  "clear",
		Usage: "/clear",
		Help:  "Clear transcript from the UI.",
		Handle: func(m *Model, _ string) (string, bool) {
			m.history = nil
			m.streamingThinking = ""
			m.streamingContent = ""
			m.toolPendingIdx = make(map[string]int)
			return "cleared transcript", false
		},
	})
	m.registerCommand(commandSpec{
		Name:  "reset",
		Usage: "/reset",
		Help:  "Reset agent state and transcript.",
		Handle: func(m *Model, _ string) (string, bool) {
			m.agent.Reset()
			m.err = nil
			m.history = nil
			m.streamingThinking = ""
			m.streamingContent = ""
			m.usageIn, m.usageOut, m.usageTotal = 0, 0, 0
			m.toolPendingIdx = make(map[string]int)
			return "reset done", false
		},
	})
	m.registerCommand(commandSpec{
		Name:  "abort",
		Usage: "/abort",
		Help:  "Abort current streaming generation.",
		Handle: func(m *Model, _ string) (string, bool) {
			m.agent.Abort()
			return "abort requested", false
		},
	})
	m.registerCommand(commandSpec{
		Name:  "model",
		Usage: "/model [model_id]",
		Help:  "List models or switch current model.",
		Handle: func(m *Model, arg string) (string, bool) {
			arg = strings.TrimSpace(arg)
			if arg != "" {
				model, ok := runtime.GetModel(arg)
				if !ok {
					return "unknown model: " + arg, false
				}
				m.agent.SetModel(model)
				return "model: " + model.Provider + "/" + model.ID, false
			}
			models := runtime.ListModels()
			sort.Slice(models, func(i, j int) bool {
				if models[i].Provider == models[j].Provider {
					return models[i].ID < models[j].ID
				}
				return models[i].Provider < models[j].Provider
			})
			current := m.agent.Model()
			var b strings.Builder
			b.WriteString("current: " + current.Provider + "/" + current.ID + "\n")
			for _, model := range models {
				b.WriteString("  " + model.Provider + "/" + model.ID)
				if model.Provider == current.Provider && model.ID == current.ID {
					b.WriteString(" (current)")
				}
				b.WriteString("\n")
			}
			return strings.TrimSuffix(b.String(), "\n"), false
		},
	})
	m.registerCommand(commandSpec{
		Name:  "session",
		Usage: "/session [new]",
		Help:  "Show session path or create a new session.",
		Handle: func(m *Model, arg string) (string, bool) {
			arg = strings.TrimSpace(strings.ToLower(arg))
			if arg == "" {
				if m.session == nil || strings.TrimSpace(m.session.Path) == "" {
					return "no session", false
				}
				return "session: " + m.session.Path, false
			}
			if arg != "new" {
				return "usage: /session [new]", false
			}
			path, err := session.NewSessionPath(filepathDirSafe(m.session.Path))
			if err != nil {
				return "create session path: " + err.Error(), false
			}
			sess, err := session.Create(path)
			if err != nil {
				return "create session: " + err.Error(), false
			}
			m.session = sess
			m.agent.Reset()
			m.err = nil
			m.history = nil
			m.streamingContent = ""
			m.streamingThinking = ""
			m.usageIn, m.usageOut, m.usageTotal = 0, 0, 0
			m.toolPendingIdx = make(map[string]int)
			return "new session: " + path, false
		},
	})
	m.registerCommand(commandSpec{
		Name:  "compact",
		Usage: "/compact",
		Help:  "Summarize old history and keep a compact context.",
		Handle: func(m *Model, _ string) (string, bool) {
			n, err := m.compactContext(context.Background())
			if err != nil {
				return "compact failed: " + err.Error(), false
			}
			return fmt.Sprintf("context compacted (%d messages)", n), false
		},
	})
	m.registerCommand(commandSpec{
		Name:  "sub",
		Usage: "/sub [list|create <id> [role]|remove <id>|run <id> <prompt>]",
		Help:  "Manage sub-agents directly.",
		Handle: func(m *Model, arg string) (string, bool) {
			return m.handleSubCommand(arg), false
		},
	})
}

func (m *Model) runCommand(raw string) (reply string, quit bool) {
	cmd, arg, ok := parseSlashCommand(raw)
	if !ok {
		return "not a command", false
	}
	spec, ok := m.commands[cmd]
	if !ok || spec.Handle == nil {
		return "unknown command: /" + cmd + " (try /help)", false
	}
	return spec.Handle(m, arg)
}

func parseSlashCommand(raw string) (cmd string, arg string, ok bool) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "/") {
		return "", "", false
	}
	parts := strings.Fields(raw)
	if len(parts) == 0 {
		return "", "", false
	}
	cmd = strings.ToLower(strings.TrimPrefix(parts[0], "/"))
	if cmd == "" {
		return "", "", false
	}
	if len(parts) > 1 {
		arg = strings.Join(parts[1:], " ")
	}
	return cmd, arg, true
}

func (m *Model) commandPaletteFromText(raw string) string {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "/") {
		return ""
	}
	query := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(raw, "/")))
	type item struct {
		usage string
		help  string
	}
	var items []item
	for _, spec := range m.commandSpecs {
		usage := spec.Usage
		if strings.TrimSpace(usage) == "" {
			usage = "/" + spec.Name
		}
		index := strings.ToLower(spec.Name + " " + strings.Join(spec.Aliases, " ") + " " + usage)
		if query != "" && !strings.Contains(index, query) {
			continue
		}
		items = append(items, item{
			usage: usage,
			help:  strings.TrimSpace(spec.Help),
		})
	}
	if len(items) == 0 {
		return ""
	}
	if len(items) > 10 {
		items = items[:10]
	}
	var b strings.Builder
	for _, item := range items {
		line := item.usage
		if item.help != "" {
			line += " - " + item.help
		}
		b.WriteString(line + "\n")
	}
	content := strings.TrimSuffix(b.String(), "\n")
	if content == "" {
		return ""
	}
	return content
}

func (m *Model) handleSubCommand(arg string) string {
	fields := strings.Fields(strings.TrimSpace(arg))
	if len(fields) == 0 || strings.ToLower(fields[0]) == "list" {
		infos := m.agent.SubAgentManager().List()
		if len(infos) == 0 {
			return "(no sub-agents)"
		}
		var b strings.Builder
		for i, info := range infos {
			if i > 0 {
				b.WriteString("\n")
			}
			if strings.TrimSpace(info.Role) == "" {
				fmt.Fprintf(&b, "%s\t%s", info.ID, info.AgentID)
			} else {
				fmt.Fprintf(&b, "%s\t%s\t%s", info.ID, info.Role, info.AgentID)
			}
		}
		return b.String()
	}
	switch strings.ToLower(fields[0]) {
	case "create":
		if len(fields) < 2 {
			return "usage: /sub create <id> [role]"
		}
		id := fields[1]
		role := ""
		if len(fields) > 2 {
			role = strings.Join(fields[2:], " ")
		}
		childID, err := m.agent.SubAgentManager().Create(context.Background(), agent.SubAgentSpec{
			ID:   id,
			Role: strings.TrimSpace(role),
		})
		if err != nil {
			return err.Error()
		}
		return "created sub-agent: " + childID
	case "remove":
		if len(fields) < 2 {
			return "usage: /sub remove <id>"
		}
		if err := m.agent.SubAgentManager().Remove(fields[1]); err != nil {
			return err.Error()
		}
		return "removed sub-agent: " + strings.TrimSpace(fields[1])
	case "run":
		if len(fields) < 3 {
			return "usage: /sub run <id> <prompt>"
		}
		id := fields[1]
		prompt := strings.TrimSpace(strings.Join(fields[2:], " "))
		out, err := m.agent.SubAgentManager().Run(context.Background(), id, prompt)
		if err != nil {
			return err.Error()
		}
		m.history = append(m.history, "Sub["+id+"]: "+strings.TrimSpace(out))
		return "sub-agent done: " + id
	default:
		return "usage: /sub [list|create <id> [role]|remove <id>|run <id> <prompt>]"
	}
}

func (m *Model) compactContext(ctx context.Context) (int, error) {
	msgs := m.agent.ContextManager().MessageSnapshot()
	if len(msgs) <= 2 {
		return len(msgs), nil
	}
	prefix, body := splitLeadingSystem(msgs)
	if len(body) <= 2 {
		return len(msgs), nil
	}

	compactSystem := communi.NewSystemMessageWithoutId("You are a concise conversation summarizer for coding tasks.")
	compactUser := communi.NewUserMessageWithoutId("Summarize key decisions, pending tasks, and important context for continuing this coding session.")
	input := append([]communi.Message{compactSystem}, body...)
	input = append(input, compactUser)

	assistant, _, err := m.agent.Provider().Complete(ctx, m.agent.Model(), input, &llm.Options{
		ToolChoice:      "none",
		MaxOutputTokens: 4000,
	})
	if err != nil {
		kept := body
		if len(kept) > 24 {
			kept = kept[len(kept)-24:]
		}
		out := append(append([]communi.Message(nil), prefix...), kept...)
		m.agent.ContextManager().ReplaceMessages(out)
		_ = m.rewriteSession(out)
		m.history = renderHistoryFromMessages(out)
		return len(out), nil
	}

	summary := strings.TrimSpace(assistant.ContentBlocksToText())
	if summary == "" {
		return 0, fmt.Errorf("empty summary")
	}
	summaryMsg := communi.NewUserMessageWithoutId("This session has been compacted. Continue from this summary:\n\n" + summary)
	out := append(append([]communi.Message(nil), prefix...), summaryMsg)
	m.agent.ContextManager().ReplaceMessages(out)
	_ = m.rewriteSession(out)
	m.history = renderHistoryFromMessages(out)
	return len(out), nil
}

func (m *Model) rewriteSession(msgs []communi.Message) error {
	if m.session == nil || strings.TrimSpace(m.session.Path) == "" {
		return nil
	}
	if err := os.WriteFile(m.session.Path, nil, 0o644); err != nil {
		return err
	}
	for _, msg := range msgs {
		if err := m.session.AppendMessage(msg); err != nil {
			return err
		}
	}
	return nil
}

func filepathDirSafe(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "."
	}
	return filepath.Dir(path)
}
