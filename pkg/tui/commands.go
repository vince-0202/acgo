package tui

import (
	"fmt"
	"github.com/vince-0202/acgo/pkg/communi"
	"github.com/vince-0202/acgo/pkg/runtime"
	"os"
	"strings"
	"time"

	"github.com/vince-0202/acgo/pkg/contextfile"
	"github.com/vince-0202/acgo/pkg/keys"
	"github.com/vince-0202/acgo/pkg/session"
	"github.com/vince-0202/acgo/pkg/skills"
)

func (m *Model) registerCommand(spec commandSpec) {
	name := strings.ToLower(strings.TrimSpace(spec.Name))
	if name == "" || spec.Handle == nil {
		return
	}
	spec.Name = name
	m.commandSpecs = append(m.commandSpecs, spec)
	m.commands[name] = spec
	for _, a := range spec.Aliases {
		alias := strings.ToLower(strings.TrimSpace(a))
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
			for _, s := range m.commandSpecs {
				usage := s.Usage
				if strings.TrimSpace(usage) == "" {
					usage = "/" + s.Name
				}
				b.WriteString("  " + usage)
				if strings.TrimSpace(s.Help) != "" {
					b.WriteString(" - " + strings.TrimSpace(s.Help))
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
		Name:  "model",
		Usage: "/model [provider/model_id]",
		Help:  "List models or switch current model.",
		Handle: func(m *Model, arg string) (string, bool) {
			arg = strings.TrimSpace(arg)
			if arg != "" {
				if mod, ok := runtime.GetModel(arg); ok {
					m.agent.SetModel(mod)
					return "model: " + mod.Provider + "/" + mod.ID, false
				}
				return "unknown model: " + arg, false
			}
			models := runtime.ListModels()
			var b strings.Builder
			cur := m.agent.Model
			b.WriteString("current: " + cur.Provider + "/" + cur.ID + "\n")
			for _, mod := range models {
				b.WriteString("  " + mod.Provider + "/" + mod.ID)
				if mod.ID == cur.ID && mod.Provider == cur.Provider {
					b.WriteString(" (current)")
				}
				b.WriteString("\n")
			}
			return strings.TrimSuffix(b.String(), "\n"), false
		},
	})

	m.registerCommand(commandSpec{
		Name:  "session",
		Usage: "/session",
		Help:  "Show current session file path.",
		Handle: func(m *Model, _ string) (string, bool) {
			if m.session != nil && m.session.Path != "" {
				return "session: " + m.session.Path, false
			}
			return "no session", false
		},
	})

	m.registerCommand(commandSpec{
		Name:  "new",
		Usage: "/new",
		Help:  "Start a new session (new file + reset chat).",
		Handle: func(m *Model, _ string) (string, bool) {
			path, err := session.NewSessionPath(m.sessionRoot)
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
			return "new session: " + path, false
		},
	})

	m.registerCommand(commandSpec{
		Name:  "name",
		Usage: "/name <title>",
		Help:  "Set a human-readable session name (stored in session file).",
		Handle: func(m *Model, arg string) (string, bool) {
			title := strings.TrimSpace(arg)
			if title == "" {
				return "usage: /name <title>", false
			}
			if m.session != nil && m.session.Path != "" {
				_ = m.session.AppendMessage(communi.Message{
					ID:        "name-" + time.Now().UTC().Format(time.RFC3339Nano),
					Role:      keys.AgentRoleNotification,
					Content:   []*communi.ContentBlock{communi.NewTextContentBlock("session name: " + title)},
					CreatedAt: time.Now().UTC(),
					Metadata: map[string]any{
						"type": "session_name",
						"name": title,
					},
				})
			}
			return "name: " + title, false
		},
	})

	m.registerCommand(commandSpec{
		Name:  "tree",
		Usage: "/tree",
		Help:  "Show a lightweight conversation outline.",
		Handle: func(m *Model, _ string) (string, bool) {
			msgs := m.agent.GetMessages()
			if len(msgs) == 0 {
				return "(empty)", false
			}
			// Keep it short for the TUI: last 40 messages.
			start := 0
			if len(msgs) > 40 {
				start = len(msgs) - 40
			}
			var b strings.Builder
			b.WriteString("messages (latest last):\n")
			for i := start; i < len(msgs); i++ {
				role := string(msgs[i].Role)
				line := fmt.Sprintf("  %d. %s", i+1, role)
				if msgs[i].ID != "" {
					line += " " + msgs[i].ID
				}
				content := strings.TrimSpace(msgs[i].ContentBlocksToText())
				if content != "" {
					if len(content) > 60 {
						content = content[:60] + "…"
					}
					line += " - " + content
				}
				b.WriteString(line + "\n")
			}
			return strings.TrimSuffix(b.String(), "\n"), false
		},
	})

	m.registerCommand(commandSpec{
		Name:  "reset",
		Usage: "/reset",
		Help:  "Reset chat (clear messages and UI state).",
		Handle: func(m *Model, _ string) (string, bool) {
			m.agent.Reset()
			m.err = nil
			m.history = nil
			m.streamingContent = ""
			m.streamingThinking = ""
			return "reset done", false
		},
	})

	m.registerCommand(commandSpec{
		Name:  "settings",
		Usage: "/settings",
		Help:  "Show settings file location.",
		Handle: func(_ *Model, _ string) (string, bool) {
			home, _ := os.UserHomeDir()
			return "config: " + home + "/.acgo/settings.yaml", false
		},
	})

	m.registerCommand(commandSpec{
		Name:  "system",
		Usage: "/system",
		Help:  "Show current system prompt summary and loaded context files.",
		Handle: func(m *Model, _ string) (string, bool) {
			p := strings.TrimSpace(m.agent.State().SystemPrompt)
			if p == "" {
				return "(system prompt empty)", false
			}
			summary := p
			if len(summary) > 400 {
				summary = summary[:400] + "…"
			}
			var b strings.Builder
			for _, ag := range runtime.ListAgents() {
				b.WriteString("\n==================\n")
				b.WriteString(fmt.Sprintf("agent: %v \n", ag.Id()))
				b.WriteString(fmt.Sprintf("system prompt: %d chars\n", len(p)))
				b.WriteString(summary + "\n")
				contextPaths := ag.State().ContextFile.Paths
				if len(contextPaths) > 0 {
					b.WriteString("\nloaded context files:\n")
					for _, cp := range contextPaths {
						b.WriteString("  " + cp + "\n")
					}
				}
			}

			b.WriteString("==================")
			return strings.TrimSuffix(b.String(), "\n"), false
		},
	})

	m.registerCommand(commandSpec{
		Name:  "skill",
		Usage: "/skill <name>",
		Help:  "Apply skill by name to the next message (model should call skill_search).",
		Handle: func(m *Model, arg string) (string, bool) {
			name := strings.TrimSpace(arg)
			if name == "" {
				return "usage: /skill <name> (e.g. /skill code-review)", false
			}
			m.pendingSkillContent = "请先调用 skill_search 工具查询该 skill（name=" + name + "）并严格遵循 SKILL.md 内容，然后再处理下面用户请求。"
			return "next message will ask model to use skill_search: " + name, false
		},
	})

	m.registerCommand(commandSpec{
		Name:  "skills",
		Usage: "/skills",
		Help:  "List available skills (from disk).",
		Handle: func(m *Model, _ string) (string, bool) {
			skillList, _ := skills.Load(m.workDir)
			if len(skillList) == 0 {
				return "no skills loaded (add SKILL.md in ~/.acgo/skills/ or .acgo/skills/)", false
			}
			var b strings.Builder
			b.WriteString("loaded skills:\n")
			for _, s := range skillList {
				b.WriteString("  " + s.Name)
				if s.Description != "" {
					b.WriteString(" - " + s.Description)
				}
				b.WriteString("\n")
			}
			return strings.TrimSuffix(b.String(), "\n"), false
		},
	})

	m.registerCommand(commandSpec{
		Name:  "reload",
		Usage: "/reload",
		Help:  "Reload context files and refresh system prompt.",
		Handle: func(m *Model, _ string) (string, bool) {
			if strings.TrimSpace(m.workDir) == "" {
				return "workdir unknown; cannot reload", false
			}

			result := strings.Builder{}
			for _, ag := range runtime.ListAgents() {
				ctxResult := contextfile.Load(ag.State().WorkDir)
				ag.SetContextFile(ctxResult)
				ag.SetSystemPrompt(ctxResult.Prompt)
				merged := strings.TrimSpace(m.agent.State().SystemPrompt)
				agReloadResult := fmt.Sprintf("reloaded: %d file(s), system prompt %d chars for agent: %v.\n", len(ctxResult.Paths), len(merged), ag.Id())
				result.WriteString(agReloadResult)
			}

			return result.String(), false
		},
	})
}
