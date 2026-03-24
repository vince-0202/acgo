package tui

import (
	"acgo/pkg/bootstrap"
	"acgo/pkg/utils"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"acgo/pkg/agent"
	"acgo/pkg/config"
	"acgo/pkg/keys"
	"acgo/pkg/llm"
	"acgo/pkg/llm/deepseek"
	"acgo/pkg/llm/openai"
	"acgo/pkg/llm/qwen"
	"acgo/pkg/log"
	"acgo/pkg/memory"
	"acgo/pkg/session"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Model struct {
	agent             *agent.Agent
	session           *session.Session
	sessionRoot       string
	sessionName       string
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
		default:
			continue
		}
	}
	return providers
}

// ModelOptions configures NewModel (session and optional initial messages).
type ModelOptions struct {
	Session         *session.Session
	InitialMessages []agent.Message
	SessionName     string
}

// NewModel constructs a minimal chat TUI model wired to the Agent.
// If opts.InitialMessages is set, the agent's history is replaced with them (e.g. after loading a session).
func NewModel(opts *ModelOptions) (*Model, error) {

	settings, err := bootstrap.Load()
	if err != nil {
		return nil, err
	}

	ag := bootstrap.BuildAgent(
		settings,
		bootstrap.WithId("tui-"+utils.NextID(keys.IdKindSnowflake)),
		bootstrap.WithDefaultTools(),
	)

	if opts != nil && len(opts.InitialMessages) > 0 {
		ag.ReplaceMessages(opts.InitialMessages)
	}

	model := &Model{
		agent:        ag,
		textarea:     newInputTA(),
		history:      nil,
		width:        80,
		height:       24,
		commands:     map[string]commandSpec{},
		commandSpecs: nil,
		workDir:      settings.WorkDir,
	}
	if opts != nil {
		model.session = opts.Session
		model.sessionName = strings.TrimSpace(opts.SessionName)
		model.history = append(model.history, renderHistoryFromMessages(opts.InitialMessages)...)
	}
	// sessionRoot is used by /new even when a session is already open.
	root := settings.Session.Root
	if root == "" {
		home, _ := os.UserHomeDir()
		root = home + "/.acgo/sessions"
	}
	model.sessionRoot = root

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

type assistantReplyMsg struct {
	text string
	err  error
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
					msg := agent.Message{Role: keys.AgentRoleUser, Content: m.textarea.Value()}
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
					msg := agent.Message{Role: keys.AgentRoleUser, Content: m.textarea.Value()}
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
				m.history = append(m.history, "Error: "+agent.FormatErrorForDisplay(msg.Err))
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
	views = append(views,
		lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(m.statusLine()),
	)
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
	parts = append(parts, st.Model.Provider+"/"+st.Model.ID)
	if st.IsStreaming {
		parts = append(parts, "streaming")
	} else {
		parts = append(parts, "idle")
	}
	if st.Error != nil {
		parts = append(parts, agent.FormatErrorForDisplay(st.Error))
	}
	status := strings.Join(parts, " | ")
	if m.session != nil && m.session.Path != "" {
		status = "Session: " + m.session.Path + " | " + status
	}
	if strings.TrimSpace(m.sessionName) != "" {
		status = "Name: " + strings.TrimSpace(m.sessionName) + " | " + status
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
		unsub := m.agent.Subscribe(func(e agent.Event) {
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

func (m *Model) handleAgentEvent(e agent.Event, done *atomic.Bool, ch chan streamEvent) {
	if done.Load() {
		return
	}
	if m.session != nil && e.Message != nil {
		switch e.Type {
		case agent.EventMessageEnd:
			_ = m.session.AppendMessage(agentMessageToSession(e.Message))
		case agent.EventToolExecutionEnd:
			_ = m.session.AppendMessage(agentMessageToSession(e.Message))
		}
	}
	switch e.Type {
	case agent.EventMessageUpdate:
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
	case agent.EventMessageEnd:
		// Streamed deltas are accumulated in UI state; when the assistant message ends
		// (often right before tool execution), flush finalized content/thinking so the
		// following tool logs appear in correct chronological order.
		if e.Message == nil || e.Message.Role != keys.AgentRoleAssistant {
			return
		}
		finalThinking := strings.TrimSpace(e.Message.Thinking)
		finalContent := strings.TrimSpace(e.Message.Content)
		if finalThinking == "" && finalContent == "" {
			return
		}
		select {
		case ch <- streamEvent{FinalThinking: finalThinking, FinalContent: finalContent, Ch: ch}:
		default:
		}
	case agent.EventToolExecutionStart:
		lines := formatToolExecutionStartLines(e.ToolName, e.ToolCallID, e.ToolArgs)
		if len(lines) == 0 {
			return
		}
		select {
		case ch <- streamEvent{ToolText: strings.Join(lines, "\n"), Ch: ch}:
		default:
		}
	case agent.EventToolExecutionEnd:
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

func formatToolExecutionEndLines(toolName string, toolCallID string, args []byte, msg *agent.Message, execErr error) []string {
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
		content = strings.TrimSpace(msg.Content)
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
	settings, err := config.LoadSettings()
	if err != nil {
		return err
	}

	sess, initial, sessName, err := loadSessionAndMessage(sessionId, settings)
	if err != nil {
		return err
	}

	model, err := NewModel(&ModelOptions{Session: sess, InitialMessages: initial, SessionName: sessName})
	if err != nil {
		return err
	}

	_, err = tea.NewProgram(model, tea.WithOutput(os.Stdout)).Run()
	return err
}

func loadSessionAndMessage(sessionId string, settings *config.Settings) (*session.Session, []agent.Message, string, error) {
	var sess *session.Session
	var initial []agent.Message
	var sessName string

	if sessionId == "" {
		path, err := session.NewSessionPath(settings.Session.Root)
		if err != nil {
			return nil, nil, "", fmt.Errorf("create session path: %w", err)
		}
		sess, err = session.Create(path)
		if err != nil {
			return nil, nil, "", fmt.Errorf("create session: %w", err)
		}
	} else {
		path := filepath.Join(settings.Session.Root, sessionId+".jsonl")
		sess = session.Open(path)
		msgs, err := sess.LoadAll()
		if err == nil && len(msgs) > 0 {
			sessName = extractSessionName(msgs)
			initial = sessionMessagesToAgent(msgs)
		}
	}
	return sess, initial, sessName, nil
}

func extractSessionName(msgs []session.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Metadata == nil {
			continue
		}
		if t, _ := msgs[i].Metadata["type"].(string); t != "session_name" {
			continue
		}
		if n, _ := msgs[i].Metadata["name"].(string); strings.TrimSpace(n) != "" {
			return strings.TrimSpace(n)
		}
	}
	return ""
}

func sessionMessagesToAgent(msgs []session.Message) []agent.Message {
	out := make([]agent.Message, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, agent.Message{
			ID:         m.ID,
			Role:       keys.AgentMessageRole(m.Role),
			Content:    m.Content,
			Thinking:   m.Thinking,
			ToolCallID: m.ToolCallID,
			IsError:    m.IsError,
			Metadata:   m.Metadata,
		})
	}
	return out
}

func renderHistoryFromMessages(msgs []agent.Message) []string {
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

func renderHistoryLinesFromMessage(m agent.Message) []string {
	content := strings.TrimSpace(m.Content)
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

func agentMessageToSession(msg *agent.Message) session.Message {
	return session.Message{
		ID:         msg.ID,
		Role:       string(msg.Role),
		Content:    msg.Content,
		Thinking:   msg.Thinking,
		ToolCallID: msg.ToolCallID,
		IsError:    msg.IsError,
		CreatedAt:  time.Now().UTC(),
		Metadata:   msg.Metadata,
	}
}
