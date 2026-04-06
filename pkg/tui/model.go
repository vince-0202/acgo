package tui

import (
	"context"
	"fmt"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/vince-0202/acgo/pkg/agent"
	agent2 "github.com/vince-0202/acgo/pkg/bootstrap/agent"
	session2 "github.com/vince-0202/acgo/pkg/bootstrap/session"
	"github.com/vince-0202/acgo/pkg/bootstrap/setting"
	"github.com/vince-0202/acgo/pkg/communi"
	"github.com/vince-0202/acgo/pkg/config"
	"github.com/vince-0202/acgo/pkg/errors"
	"github.com/vince-0202/acgo/pkg/keys"
	"github.com/vince-0202/acgo/pkg/llm"
	"github.com/vince-0202/acgo/pkg/llm/deepseek"
	"github.com/vince-0202/acgo/pkg/llm/glm"
	"github.com/vince-0202/acgo/pkg/llm/openai"
	"github.com/vince-0202/acgo/pkg/llm/qwen"
	"github.com/vince-0202/acgo/pkg/log"
	"github.com/vince-0202/acgo/pkg/memory"
	"github.com/vince-0202/acgo/pkg/session"
	"github.com/vince-0202/acgo/pkg/utils"
	"os"
	"strings"
	"sync/atomic"
)

type Model struct {
	agent             *agent.Agent
	session           *session.Session
	sessionRoot       string
	textarea          textarea.Model
	history           []string
	streamingContent  string
	streamingThinking string
	streamCh          chan streamEvent
	width             int
	height            int
	err               error

	// command registry (P1.2): maps name/alias -> handler
	commands     map[string]commandSpec
	commandSpecs []commandSpec // stable list for /help

	// context files (P1.3)
	workDir string

	// pendingSkillContent: when set, next user message is prefixed with this (for /skill:name).
	pendingSkillContent string
}

// streamEvent is sent from the agent goroutine for each delta or done/error.
type streamEvent struct {
	Delta         string // text content delta
	ThinkingDelta string // reasoning/thinking delta (pi-ai thinking_* events)
	ToolText      string // human-friendly tool execution log line(s)
	FinalContent  string // finalized assistant content at message end (ordered output)
	FinalThinking string // finalized assistant thinking at message end (ordered output)
	Done          bool
	Err           error
	Ch            chan streamEvent
}

type commandHandler func(m *Model, arg string) (reply string, quit bool)

type commandSpec struct {
	Name    string
	Aliases []string
	Usage   string
	Help    string
	Handle  commandHandler
}

func newProviderBySettings(settings config.AgentSetting) []llm.Provider {
	var providers []llm.Provider
	for _, providerSetting := range settings.Providers {
		switch providerSetting.Provider {
		case keys.ProviderTypeOpenAi:
			providers = append(providers, openai.NewClient(providerSetting))
		case keys.ProviderTypeDeepSeek:
			providers = append(providers, deepseek.NewClient(providerSetting))
		case keys.ProviderTypeQwen:
			providers = append(providers, qwen.NewClient(providerSetting))
		//case keys.ProviderTypeAnthropic:
		//	providers = append(providers, anthropic.NewClient(providerSetting))
		//case keys.ProviderTypeGemini:
		//	providers = append(providers, gemini.NewClient(providerSetting))
		case keys.ProviderTypeGLM:
			providers = append(providers, glm.NewClient(providerSetting))
		default:
			continue
		}
	}
	return providers
}

// ModelOptions configures NewModel (session and optional initial messages).
type ModelOptions struct {
	Settings *config.Settings
	Session  *session.Session
}

// NewModel constructs a minimal chat TUI model wired to the Agent.
// If opts.InitialMessages is set, the agent's history is replaced with them (e.g. after loading a session).
func NewModel(opts *ModelOptions) (*Model, error) {

	ag := agent2.BuildAgent(
		opts.Settings,
		agent2.WithId("tui-"+utils.NextID(keys.IdKindSnowflake)),
		agent2.WithDefaultTools(),
	)

	model := &Model{
		agent:        ag,
		textarea:     newInputTA(),
		history:      nil,
		width:        80,
		height:       24,
		commands:     map[string]commandSpec{},
		commandSpecs: nil,
		workDir:      opts.Settings.WorkDir,
	}

	model.session = opts.Session
	model.sessionRoot = opts.Settings.Session.Root
	if len(opts.Session.Message) > 0 {
		ag.ReplaceMessages(opts.Session.Message)
		model.history = append(model.history, renderHistoryFromMessages(opts.Session.Message)...)
	}

	model.registerBuiltinCommands()
	return model, nil
}

func newInputTA() textarea.Model {
	ta := textarea.New()
	ta.Placeholder = "Ask acgo about your thoughts..."
	ta.SetHeight(1)
	ta.ShowLineNumbers = false
	ta.Focus()
	return ta
}

func (m Model) Init() tea.Cmd {
	return textarea.Blink
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.width <= 0 {
			m.width = 80
		}
		m.textarea.SetWidth(msg.Width)
		return m, nil
	case tea.KeyMsg:
		st := m.agent.State()
		switch msg.String() {
		case "ctrl+c", "esc":
			if st.IsStreaming {
				m.agent.Abort()
				return m, nil
			}
			return m, tea.Quit
		case "enter":
			if m.textarea.Focused() {
				input := strings.TrimSpace(m.textarea.Value())
				if input == "" {
					return m, nil
				}
				if st.IsStreaming {
					// P0.4: Agent 忙时 Enter = steering 入队
					msg := communi.NewUserMessageWithoutId(m.textarea.Value())
					m.agent.EnqueueSteering(msg)
					m.textarea.SetValue("")
					m.history = append(m.history, "You: (steering) "+input)
					return m, nil
				}
				if strings.HasPrefix(input, "/") {
					// P0.2: 命令解析
					reply, quit := m.runCommand(m.textarea.Value())
					m.textarea.SetValue("")
					if quit {
						return m, tea.Quit
					}
					m.history = append(m.history, "> "+reply)
					return m, nil
				}
				m.textarea.SetValue("")
				if m.pendingSkillContent != "" {
					input = m.pendingSkillContent + "\n\n---\n\n" + input
					m.pendingSkillContent = ""
				}
				log.Debugf("send prompt len=%d", len(input))
				m.history = append(m.history, "You: "+input)
				return m, m.runAgentStream(input)
			}
		case "alt+enter":
			if m.textarea.Focused() {
				input := strings.TrimSpace(m.textarea.Value())
				if input == "" {
					return m, nil
				}
				if st.IsStreaming {
					// P0.4: Agent 忙时 Alt+Enter = follow-up 入队
					msg := communi.NewUserMessageWithoutId(m.textarea.Value())
					m.agent.EnqueueFollowUp(msg)
					m.textarea.SetValue("")
					m.history = append(m.history, "You: (follow-up) "+input)
					return m, nil
				}
				// 非 streaming 时 alt+enter 不发送，交给 textarea 处理换行
			}
		}
	case streamEvent:
		// Flush finalized assistant chunks in chronological order.
		// This makes tool logs interleave correctly with thinking/content between tool calls.
		if msg.FinalThinking != "" {
			m.history = append(m.history, "[Thinking] "+msg.FinalThinking)
			m.streamingThinking = ""
		}
		if msg.FinalContent != "" {
			m.history = append(m.history, "Assistant: "+msg.FinalContent)
			m.streamingContent = ""
		}
		if msg.Done || msg.Err != nil {
			if msg.Err != nil {
				log.Debugf("stream done err=%v", msg.Err)
				m.err = msg.Err
				m.history = append(m.history, "Error: "+errors.FormatErrorForDisplay(msg.Err))
			} else {
				log.Debugf("stream done content_len=%d thinking_len=%d", len(m.streamingContent), len(m.streamingThinking))
				if m.streamingThinking != "" {
					m.history = append(m.history, "[Thinking] "+m.streamingThinking)
				}
				if m.streamingContent != "" {
					m.history = append(m.history, "Assistant: "+m.streamingContent)
				}
			}
			m.streamingContent = ""
			m.streamingThinking = ""
			return m, nil
		}
		if msg.ToolText != "" {
			// Tool execution logs are emitted from agent goroutine; only append to history here (UI thread).
			m.history = append(m.history, strings.Split(msg.ToolText, "\n")...)
		}
		if msg.ThinkingDelta != "" {
			m.streamingThinking += msg.ThinkingDelta
		}
		if msg.Delta != "" {
			m.streamingContent += msg.Delta
		}
		// Schedule next read from the same channel.
		return m, m.waitForStreamEvent(msg.Ch)
	}

	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	return m, cmd
}

// thinkingStyle is used for Thinking label and content: lighter gray, faint for a secondary look.
var thinkingStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("246")).
	Faint(true)

func (m Model) View() string {

	w := m.width
	if w <= 0 {
		w = 80
	}
	wrapWidth := w - 2
	if wrapWidth < 20 {
		wrapWidth = 20
	}

	body := ""
	for _, line := range m.history {
		if strings.HasPrefix(line, "[Thinking] ") {
			body += thinkingStyle.Render(line) + "\n"
		} else {
			body += line + "\n"
		}
	}
	if m.streamingThinking != "" {
		body += thinkingStyle.Render("[Thinking] "+m.streamingThinking+"▌") + "\n"
	}
	if m.streamingContent != "" {
		body += "Assistant: " + m.streamingContent + "▌"
	}
	if body == "" {
		body = " "
	}

	// When typing a slash command, show a palette under the input.
	var commandView string
	if v := strings.TrimSpace(m.textarea.Value()); strings.HasPrefix(v, "/") {
		commandView = m.commandPaletteView()
	}

	views := []string{
		lipgloss.NewStyle().Width(wrapWidth).Render(body),
		lipgloss.NewStyle().Border(lipgloss.DoubleBorder(), true, false, true, false).Render(m.textarea.View()),
	}
	if commandView != "" {
		views = append(views, commandView)
	}
	views = append(views, lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(m.statusLine()))

	return lipgloss.JoinVertical(lipgloss.Left, views...)
}

// commandPaletteView renders a filtered list of slash commands under the input.
// It uses substring match on name, aliases, and usage.
func (m Model) commandPaletteView() string {
	raw := strings.TrimSpace(m.textarea.Value())
	if !strings.HasPrefix(raw, "/") {
		return ""
	}
	// query is content after first "/"
	query := strings.TrimSpace(strings.TrimPrefix(raw, "/"))
	queryLower := strings.ToLower(query)

	type item struct {
		usage string
		help  string
		name  string
	}
	var items []item
	for _, spec := range m.commandSpecs {
		usage := spec.Usage
		if strings.TrimSpace(usage) == "" {
			usage = "/" + spec.Name
		}
		text := strings.ToLower(spec.Name + " " + strings.Join(spec.Aliases, " ") + " " + usage)
		if queryLower != "" && !strings.Contains(text, queryLower) {
			continue
		}
		items = append(items, item{
			usage: usage,
			help:  strings.TrimSpace(spec.Help),
			name:  spec.Name,
		})
	}
	if len(items) == 0 {
		return ""
	}
	// Limit number of rows so palette不会盖满屏幕
	maxRows := 10
	if len(items) > maxRows {
		items = items[:maxRows]
	}

	var b strings.Builder
	for _, it := range items {
		line := it.usage
		if it.help != "" {
			line += " - " + it.help
		}
		b.WriteString(line + "\n")
	}
	content := strings.TrimSuffix(b.String(), "\n")
	if content == "" {
		return ""
	}
	// Palette style: 单独边框 + 略微变暗的前景色
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	return style.Render(content)
}

func (m *Model) runCommand(raw string) (reply string, quit bool) {
	cmd, arg, ok := parseSlashCommand(raw)
	if !ok {
		return "not a command", false
	}
	// Support /skill:name form: treat "skill:code-review" as cmd=skill, arg=code-review
	if i := strings.Index(cmd, ":"); i >= 0 && i < len(cmd)-1 {
		if arg == "" {
			arg = strings.TrimSpace(cmd[i+1:])
		} else {
			arg = strings.TrimSpace(cmd[i+1:]) + " " + arg
		}
		cmd = cmd[:i]
	}
	spec, ok := m.commands[cmd]
	if !ok || spec.Handle == nil {
		return "unknown command: /" + cmd + " (try /help)", false
	}
	return spec.Handle(m, arg)
}

// statusLine returns a one-line status: model, streaming, last error (P0.5).
func (m *Model) statusLine() string {
	st := m.agent.State()
	var parts []string
	parts = append(parts, m.agent.Provider.Name()+"/"+m.agent.Model.ID)
	if st.IsStreaming {
		parts = append(parts, "streaming")
	} else {
		parts = append(parts, "idle")
	}
	if st.Error != nil {
		parts = append(parts, errors.FormatErrorForDisplay(st.Error))
	}
	status := strings.Join(parts, " | ")
	if m.session != nil && m.session.Path != "" {
		status = "Session: " + m.session.Path + " | " + status
	}

	return status
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

// waitForStreamEvent returns a Cmd that reads one event from ch (for streaming).
func (m *Model) waitForStreamEvent(ch chan streamEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return streamEvent{Done: true, Ch: ch}
		}
		return ev
	}
}

// runAgentStream runs Prompt in a goroutine and streams message_update deltas to the TUI.
// When log_level is debug, LLM logs are written to the same log file as TUI debug.
func (m *Model) runAgentStream(prompt string) tea.Cmd {
	log.Debugf("runAgentStream start")
	ch := make(chan streamEvent, 64)
	var done atomic.Bool
	go func() {
		unsub := m.agent.Subscribe(func(e communi.AgentEvent) {
			m.handleAgentEvent(e, &done, ch)
		})
		defer unsub()

		ctx := context.Background()
		if m.session != nil && strings.TrimSpace(m.session.Path) != "" {
			ctx = memory.WithSessionID(ctx, m.session.Path)
		}
		err := m.agent.Prompt(ctx, prompt)
		done.Store(true) // block further sends before closing
		unsub()
		ch <- streamEvent{Done: true, Err: err, Ch: ch}
		close(ch)
	}()
	// Block until first event (delta or done), then return it; Update will schedule next read via waitForStreamEvent.
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return streamEvent{Done: true, Ch: ch}
		}
		return ev
	}
}

func (m *Model) handleAgentEvent(e communi.AgentEvent, done *atomic.Bool, ch chan streamEvent) {
	if done.Load() {
		return
	}
	if m.session != nil && e.Message != nil {
		switch e.Type {
		case communi.EventMessageEnd:
			_ = m.session.AppendMessage(*e.Message)
		case communi.EventToolExecutionEnd:
			_ = m.session.AppendMessage(*e.Message)
		}
	}
	switch e.Type {
	case communi.EventMessageUpdate:
		if e.LlmEvent == nil {
			return
		}
		if e.LlmEvent.TextDelta != "" {
			select {
			case ch <- streamEvent{Delta: e.LlmEvent.TextDelta, Ch: ch}:
			default:
			}
		}
		if e.LlmEvent.ThinkingDelta != "" {
			select {
			case ch <- streamEvent{ThinkingDelta: e.LlmEvent.ThinkingDelta, Ch: ch}:
			default:
			}
		}
	case communi.EventMessageEnd:
		// Streamed deltas are accumulated in UI state; when the assistant message ends
		// (often right before tool execution), flush finalized content/thinking so the
		// following tool logs appear in correct chronological order.
		if e.Message == nil || e.Message.Role != keys.AgentRoleAssistant {
			return
		}
		finalThinking := strings.TrimSpace(e.Message.Thinking)
		finalContent := strings.TrimSpace(e.Message.ContentBlocksToText())
		if finalThinking == "" && finalContent == "" {
			return
		}
		select {
		case ch <- streamEvent{FinalThinking: finalThinking, FinalContent: finalContent, Ch: ch}:
		default:
		}
	case communi.EventToolExecutionStart:
		lines := formatToolExecutionStartLines(e.ToolName, e.ToolCallID, e.ToolArgs)
		if len(lines) == 0 {
			return
		}
		select {
		case ch <- streamEvent{ToolText: strings.Join(lines, "\n"), Ch: ch}:
		default:
		}
	case communi.EventToolExecutionEnd:
		name := strings.TrimSpace(e.ToolName)
		if name == "" {
			name = "unknown"
		}
		lines := formatToolExecutionEndLines(name, e.ToolCallID, e.ToolArgs, e.Message, e.Error)
		if len(lines) == 0 {
			return
		}
		select {
		case ch <- streamEvent{ToolText: strings.Join(lines, "\n"), Ch: ch}:
		default:
		}
	}
}

func formatToolExecutionStartLines(toolName string, toolCallID string, args []byte) []string {
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		return nil
	}
	id := strings.TrimSpace(toolCallID)

	// Claude-code-like: show tool name + args as an indented block.
	head := "Tool > " + toolName
	if id != "" {
		head += " (" + id + ")"
	}
	lines := []string{head}

	argText := strings.TrimSpace(string(args))
	if argText != "" && argText != "null" {
		lines = append(lines, "  args: "+argText)
	}
	return lines
}

func formatToolExecutionEndLines(toolName string, toolCallID string, args []byte, msg *communi.Message, execErr error) []string {
	const maxPreviewChars = 500
	const maxPreviewLines = 12

	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		toolName = "unknown"
	}
	id := strings.TrimSpace(toolCallID)

	// Prefer tool result content from message; fall back to execErr.
	content := ""
	isError := false
	if msg != nil {
		content = strings.TrimSpace(msg.ContentBlocksToText())
		isError = msg.IsError
	}
	if content == "" && execErr != nil {
		content = strings.TrimSpace(execErr.Error())
		isError = true
	}

	status := "ok"
	if isError || execErr != nil {
		status = "error"
	}

	// Claude-code-like: show end marker, and include args/output in the block.
	head := fmt.Sprintf("Tool < %s (%s)", toolName, status)
	if id != "" {
		head += " (" + id + ")"
	}
	lines := []string{head}

	argText := strings.TrimSpace(string(args))
	if argText != "" && argText != "null" {
		lines = append(lines, "  args: "+argText)
	}

	if content == "" {
		return lines
	}

	preview := content
	// line cap first (keeps structure), then char cap.
	if split := strings.Split(preview, "\n"); len(split) > maxPreviewLines {
		preview = strings.Join(split[:maxPreviewLines], "\n") + "\n…"
	}
	if len(preview) > maxPreviewChars {
		preview = preview[:maxPreviewChars] + "…"
	}

	// Indent preview for readability.
	for _, line := range strings.Split(preview, "\n") {
		if strings.TrimSpace(line) == "" {
			lines = append(lines, "  ")
			continue
		}
		lines = append(lines, "  out: "+line)
	}
	return lines
}

// Run launches the TUI. If sessionPath is empty, a new session file is created under config session root.
// If sessionPath is set, that file is opened and messages are loaded into the agent.
func Run(sessionId string) error {

	settings, err := setting.LoadAndRuntimeInit()
	if err != nil {
		return err
	}

	sess, err := session2.LoadSession(sessionId, settings.Session)
	if err != nil {
		return err
	}

	model, err := NewModel(&ModelOptions{Settings: settings, Session: sess})
	if err != nil {
		return err
	}

	_, err = tea.NewProgram(model, tea.WithOutput(os.Stdout)).Run()
	return err
}

func renderHistoryFromMessages(msgs []communi.Message) []string {
	if len(msgs) == 0 {
		return nil
	}
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		lines := renderHistoryLinesFromMessage(m)
		if len(lines) == 0 {
			continue
		}
		out = append(out, lines...)
	}
	return out
}

func renderHistoryLinesFromMessage(m communi.Message) []string {
	content := strings.TrimSpace(m.ContentBlocksToText())
	thinking := strings.TrimSpace(m.Thinking)

	switch m.Role {
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
		// Hide technical session metadata notifications from default chat transcript.
		return nil
	default:
		if content == "" {
			return nil
		}
		return []string{string(m.Role) + ": " + content}
	}
}
