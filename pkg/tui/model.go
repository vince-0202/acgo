package tui

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	xterm "github.com/charmbracelet/x/term"
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
)

// panelFrameExtra is extra lines for a framed agent panel: top border + title + bottom border.
const panelFrameExtra = 3

func detectTerminalWidth(defaultWidth int) int {
	fds := []uintptr{
		os.Stdin.Fd(),
		os.Stdout.Fd(),
		os.Stderr.Fd(),
	}
	for _, fd := range fds {
		if !xterm.IsTerminal(fd) {
			continue
		}
		w, _, err := xterm.GetSize(fd)
		if err == nil && w > 0 {
			return w
		}
	}

	v := strings.TrimSpace(os.Getenv("COLUMNS"))
	if v == "" {
		return defaultWidth
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return defaultWidth
	}
	return n
}

func detectTerminalHeight(defaultHeight int) int {
	fds := []uintptr{
		os.Stdin.Fd(),
		os.Stdout.Fd(),
		os.Stderr.Fd(),
	}
	for _, fd := range fds {
		if !xterm.IsTerminal(fd) {
			continue
		}
		_, h, err := xterm.GetSize(fd)
		if err == nil && h > 0 {
			return h
		}
	}
	v := strings.TrimSpace(os.Getenv("LINES"))
	if v == "" {
		return defaultHeight
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return defaultHeight
	}
	return n
}

func initialWindowSizeCmd() tea.Cmd {
	return func() tea.Msg {
		return tea.WindowSizeMsg{
			Width:  detectTerminalWidth(80),
			Height: detectTerminalHeight(24),
		}
	}
}

func truncateToWidth(s string, maxWidth int) string {
	s = stripANSI(s)
	if maxWidth <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= maxWidth {
		return s
	}
	if maxWidth == 1 {
		return "…"
	}
	runes := []rune(s)
	var b strings.Builder
	for _, r := range runes {
		next := b.String() + string(r)
		if lipgloss.Width(next)+1 > maxWidth {
			break
		}
		b.WriteRune(r)
	}
	return b.String() + "…"
}

func clipToWidth(s string, maxWidth int) string {
	s = stripANSI(s)
	if maxWidth <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= maxWidth {
		return s
	}
	runes := []rune(s)
	var b strings.Builder
	for _, r := range runes {
		next := b.String() + string(r)
		if lipgloss.Width(next) > maxWidth {
			break
		}
		b.WriteRune(r)
	}
	return b.String()
}

var ansiEscapeRe = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

func stripANSI(s string) string {
	return ansiEscapeRe.ReplaceAllString(s, "")
}

type Model struct {
	agent             *agent.Agent
	session           *session.Session
	sessionRoot       string
	textarea          textarea.Model
	history           []string
	streamingContent  string
	streamingThinking string
	streamCh          chan streamEvent
	viewport          viewport.Model
	autoFollow        bool
	width             int
	height            int
	err               error

	// Sub-agents: when any exist, transcript splits (main viewport + sub grid).
	subAgentPanels  map[string]*subAgentPanelState
	subAgentSubKeys map[string]func()         // "subID|chPtr" -> unsubscribe
	streamDone      *atomic.Bool              // current runAgentStream done flag (for sub subscriptions)
	subViewports    map[string]viewport.Model // one scrollable viewport per sub-agent
	scrollFocus     int                       // 0 = main agent; 1..N = sub-agent index in sorted list (Tab to cycle)
	subScrollLocked map[string]bool           // subID -> user scrolled away (disable auto-follow until new stream)

	// command registry (P1.2): maps name/alias -> handler
	commands     map[string]commandSpec
	commandSpecs []commandSpec // stable list for /help

	// context files (P1.3)
	workDir string

	// pendingSkillContent: when set, next user message is prefixed with this (for /skill <name>).
	pendingSkillContent string

	// Session token totals (summed from each LLM stream turn via EventTurnEnd).
	usageIn    int
	usageOut   int
	usageTotal int
}

// streamEvent is sent from the agent goroutine for each delta or done/error.
type streamEvent struct {
	Delta         string         // text content delta
	ThinkingDelta string         // reasoning/thinking delta (pi-ai thinking_* events)
	ToolText      string         // human-friendly tool execution log line(s)
	FinalContent  string         // finalized assistant content at message end (ordered output)
	FinalThinking string         // finalized assistant thinking at message end (ordered output)
	TurnUsage     *communi.Usage // per LLM call, from EventTurnEnd (may occur multiple times per user message)
	Done          bool
	Err           error
	Ch            chan streamEvent
	// SubID empty means main agent; non-empty routes transcript to a sub-agent panel.
	SubID string
}

// subAgentPanelState mirrors main transcript state for one delegated agent.
type subAgentPanelState struct {
	history           []string
	streamingContent  string
	streamingThinking string
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
		agent:           ag,
		textarea:        newInputTA(),
		history:         nil,
		viewport:        viewport.New(78, 16),
		autoFollow:      true,
		width:           detectTerminalWidth(80),
		height:          24,
		commands:        map[string]commandSpec{},
		commandSpecs:    nil,
		workDir:         opts.Settings.WorkDir,
		subAgentPanels:  make(map[string]*subAgentPanelState),
		subAgentSubKeys: make(map[string]func()),
		subViewports:    make(map[string]viewport.Model),
		subScrollLocked: make(map[string]bool),
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

func (m *Model) Init() tea.Cmd {
	// Request an initial size explicitly so first paint doesn't rely on defaults.
	return tea.Batch(textarea.Blink, tea.WindowSize(), initialWindowSizeCmd())
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.width <= 0 {
			m.width = detectTerminalWidth(80)
		}
		taWidth := m.width - 1
		if taWidth < 20 {
			taWidth = 20
		}
		m.textarea.SetWidth(taWidth)
		wrapWidth := m.width - 2
		if wrapWidth < 20 {
			wrapWidth = 20
		}
		m.viewport.Width = wrapWidth
		// View() sets viewport height from remaining rows after status, input, token, and optional palette.
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
					m.autoFollow = true
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
					m.autoFollow = true
					return m, nil
				}
				m.textarea.SetValue("")
				if m.pendingSkillContent != "" {
					input = m.pendingSkillContent + "\n\n---\n\n" + input
					m.pendingSkillContent = ""
				}
				log.Debugf("send prompt len=%d", len(input))
				m.history = append(m.history, "You: "+input)
				m.autoFollow = true
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
					m.autoFollow = true
					return m, nil
				}
				// 非 streaming 时 alt+enter 不发送，交给 textarea 处理换行
			}
		case "shift+tab":
			if m.textarea.Focused() {
				break
			}
			return m, m.cycleScrollFocus()
		case "pgup":
			m.scrollFocusedViewport(10, true)
			return m, nil
		case "pgdown":
			m.scrollFocusedViewport(10, false)
			return m, nil
		case "home":
			m.scrollFocusedGotoTop()
			return m, nil
		case "end":
			m.scrollFocusedGotoBottom()
			return m, nil
		case "up":
			m.scrollFocusedViewport(1, true)
			return m, nil
		case "down":
			m.scrollFocusedViewport(1, false)
			return m, nil
		}
	case tea.MouseMsg:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.scrollFocusedViewport(3, true)
			return m, nil
		case tea.MouseButtonWheelDown:
			m.scrollFocusedViewport(3, false)
			return m, nil
		}
	case streamEvent:
		if msg.Ch != nil && m.streamDone != nil {
			m.syncSubAgentSubscriptions(msg.Ch, m.streamDone)
		}
		if msg.TurnUsage != nil {
			m.accumulateSessionUsage(msg.TurnUsage)
		}
		if msg.SubID != "" {
			m.applyStreamEventToSub(msg)
			return m, m.waitForStreamEvent(msg.Ch)
		}
		// Flush finalized assistant chunks in chronological order.
		// This makes tool logs interleave correctly with thinking/content between tool calls.
		if msg.FinalThinking != "" {
			m.history = append(m.history, "[Thinking] "+msg.FinalThinking)
			m.streamingThinking = ""
			m.autoFollow = true
		}
		if msg.FinalContent != "" {
			m.history = append(m.history, "Assistant: "+msg.FinalContent)
			m.streamingContent = ""
			m.autoFollow = true
		}
		if msg.Done || msg.Err != nil {
			if msg.Err != nil {
				log.Debugf("stream done err=%v", msg.Err)
				m.err = msg.Err
				m.history = append(m.history, "Error: "+errors.FormatErrorForDisplay(msg.Err))
				m.autoFollow = true
			} else {
				log.Debugf("stream done content_len=%d thinking_len=%d", len(m.streamingContent), len(m.streamingThinking))
				if m.streamingThinking != "" {
					m.history = append(m.history, "[Thinking] "+m.streamingThinking)
				}
				if m.streamingContent != "" {
					m.history = append(m.history, "Assistant: "+m.streamingContent)
				}
				m.autoFollow = true
			}
			m.streamingContent = ""
			m.streamingThinking = ""
			return m, nil
		}
		if msg.ToolText != "" {
			// Tool execution logs are emitted from agent goroutine; only append to history here (UI thread).
			m.history = append(m.history, strings.Split(msg.ToolText, "\n")...)
			m.autoFollow = true
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

func renderAgentPanel(totalWidth int, title string, bodyView string) string {
	if totalWidth < 8 {
		totalWidth = 8
	}
	contentWidth := totalWidth - 4 // border + single-space padding on both sides
	if contentWidth < 1 {
		contentWidth = 1
	}

	fit := func(s string) string {
		s = clipToWidth(s, contentWidth)
		w := lipgloss.Width(s)
		if w < contentWidth {
			s += strings.Repeat(" ", contentWidth-w)
		}
		return s
	}

	titleText := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("245")).Render(title)
	lines := []string{fit(titleText)}

	body := lipgloss.NewStyle().Width(contentWidth).Render(bodyView)
	for _, ln := range strings.Split(body, "\n") {
		lines = append(lines, fit(ln))
	}
	if len(lines) == 1 {
		lines = append(lines, strings.Repeat(" ", contentWidth))
	}

	var b strings.Builder
	b.WriteString("┌" + strings.Repeat("─", totalWidth-2) + "┐\n")
	for _, ln := range lines {
		b.WriteString("│ " + ln + " │\n")
	}
	b.WriteString("└" + strings.Repeat("─", totalWidth-2) + "┘")
	return b.String()
}

func normalizeBlockWidth(block string, width int) string {
	if width <= 0 {
		return ""
	}
	lines := strings.Split(block, "\n")
	for i, ln := range lines {
		ln = clipToWidth(ln, width)
		w := lipgloss.Width(ln)
		if w < width {
			ln += strings.Repeat(" ", width-w)
		}
		lines[i] = ln
	}
	return strings.Join(lines, "\n")
}

func renderTranscriptBody(history []string, streamThink, streamContent string) string {
	var b strings.Builder
	for _, line := range history {
		b.WriteString(stripANSI(line) + "\n")
	}
	if streamThink != "" {
		b.WriteString("[Thinking] " + stripANSI(streamThink) + "▌\n")
	}
	if streamContent != "" {
		b.WriteString("Assistant: " + stripANSI(streamContent) + "▌")
	}
	s := b.String()
	if strings.TrimSpace(s) == "" {
		return " "
	}
	return s
}

func (m *Model) sortedSubIDs() []string {
	sac := m.agent.SubAgentController()
	if sac == nil {
		return nil
	}
	var out []string
	for _, info := range sac.List() {
		out = append(out, info.SubID)
	}
	return out
}

func (m *Model) ensureSubViewport(subID string) viewport.Model {
	if vp, ok := m.subViewports[subID]; ok {
		return vp
	}
	vp := viewport.New(1, 1)
	m.subViewports[subID] = vp
	return vp
}

func (m *Model) View() string {
	w := m.width
	if w <= 0 {
		w = 80
	}
	// Some terminals don't deliver resize events reliably; keep a safe live fallback.
	if liveW := detectTerminalWidth(w); liveW > w {
		w = liveW
	}
	// Keep one-column safety margin to avoid terminal auto-wrap when a border
	// lands on the last column (which can visually "break" horizontal borders).
	wrapWidth := w - 1
	if wrapWidth < 20 {
		wrapWidth = 20
	}

	m.pruneSubAgentPanels()

	mainBody := renderTranscriptBody(m.history, m.streamingThinking, m.streamingContent)

	// When typing a slash command, show a palette under the input.
	var commandView string
	if v := strings.TrimSpace(m.textarea.Value()); strings.HasPrefix(v, "/") {
		commandView = m.commandPaletteView()
	}

	h := m.height
	if h <= 0 {
		h = 24
	}

	// Pre-render chrome so we can count lines. The viewport height must subtract *all* fixed rows
	// (including the command palette); otherwise total layout exceeds the terminal and lines wrap,
	// which looks like a split list, duplicate bars, and leftover "/" rows.
	statusStyle := lipgloss.NewStyle().Width(wrapWidth).Foreground(lipgloss.Color("240"))
	statusStr := statusStyle.Render(truncateToWidth(m.statusLine(), wrapWidth))
	statusLines := strings.Count(statusStr, "\n") + 1

	inputStr := lipgloss.NewStyle().Border(lipgloss.DoubleBorder(), true, false, true, false).Render(m.textarea.View())
	inputLines := strings.Count(inputStr, "\n") + 1

	tokenStr := lipgloss.NewStyle().Foreground(lipgloss.Color("242")).Faint(true).Render(m.tokenSummaryLine())
	tokenLines := strings.Count(tokenStr, "\n") + 1

	paletteLines := 0
	if commandView != "" {
		paletteLines = strings.Count(commandView, "\n") + 1
	}

	chromeLines := statusLines + inputLines + tokenLines + paletteLines
	vpHeight := h - chromeLines
	if vpHeight < 1 {
		vpHeight = 1
	}

	nSub := 0
	if sac := m.agent.SubAgentController(); sac != nil {
		nSub = len(sac.List())
	}
	if nSub > 0 && m.scrollFocus > nSub {
		m.scrollFocus = nSub
	}
	if nSub == 0 {
		m.scrollFocus = 0
	}

	innerW := wrapWidth - 4
	if innerW < 16 {
		innerW = wrapWidth
	}

	if nSub > 0 {
		mainBoxH := vpHeight * 55 / 100
		subRegionH := vpHeight - mainBoxH - 1
		if mainBoxH < panelFrameExtra+3 {
			mainBoxH = panelFrameExtra + 3
		}
		if subRegionH < panelFrameExtra+4 {
			subRegionH = panelFrameExtra + 4
			mainBoxH = vpHeight - subRegionH - 1
			if mainBoxH < panelFrameExtra+3 {
				mainBoxH = panelFrameExtra + 3
			}
		}
		mainInnerH := mainBoxH - panelFrameExtra
		if mainInnerH < 3 {
			mainInnerH = 3
		}

		m.viewport.Width = innerW
		m.viewport.Height = mainInnerH
		m.viewport.SetContent(lipgloss.NewStyle().Width(innerW).Render(mainBody))
		if m.autoFollow {
			m.viewport.GotoBottom()
		}
		mainBox := normalizeBlockWidth(renderAgentPanel(wrapWidth, " Main agent", m.viewport.View()), wrapWidth)

		sepLabel := " Sub-agents "
		sepMid := strings.Repeat("─", max(0, wrapWidth-lipgloss.Width(sepLabel)))
		sep := lipgloss.NewStyle().Width(wrapWidth).Foreground(lipgloss.Color("240")).Render(sepLabel + sepMid)

		grid := m.subAgentGridView(wrapWidth, subRegionH)
		grid = lipgloss.NewStyle().Width(wrapWidth).Height(subRegionH).Render(grid)

		views := []string{
			statusStr,
			mainBox,
			sep,
			grid,
			inputStr,
			tokenStr,
		}
		if commandView != "" {
			views = append(views, commandView)
		}
		joined := lipgloss.JoinVertical(lipgloss.Left, views...)
		return joined
	}

	m.viewport.Width = innerW
	m.viewport.Height = max(3, vpHeight-panelFrameExtra)
	m.viewport.SetContent(lipgloss.NewStyle().Width(innerW).Render(mainBody))
	if m.autoFollow {
		m.viewport.GotoBottom()
	}
	mainBox := normalizeBlockWidth(renderAgentPanel(wrapWidth, " Main agent", m.viewport.View()), wrapWidth)

	views := []string{
		statusStr,
		mainBox,
		inputStr,
		tokenStr,
	}
	if commandView != "" {
		views = append(views, commandView)
	}

	joined := lipgloss.JoinVertical(lipgloss.Left, views...)
	return joined
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
	nSub := 0
	if sac := m.agent.SubAgentController(); sac != nil {
		nSub = len(sac.List())
	}
	if nSub > 0 {
		focus := "main"
		if m.scrollFocus > 0 {
			ids := m.sortedSubIDs()
			if m.scrollFocus <= len(ids) {
				focus = "sub:" + ids[m.scrollFocus-1]
			}
		}
		parts = append(parts, "scroll:"+focus+"(Shift+Tab)")
	}
	status := strings.Join(parts, " | ")
	if m.session != nil && m.session.Path != "" {
		status = "Session: " + m.session.Path + " | " + status
	}

	return status
}

func (m *Model) accumulateSessionUsage(u *communi.Usage) {
	if u == nil {
		return
	}
	m.usageIn += u.InputTokens
	m.usageOut += u.OutputTokens
	if u.TotalTokens > 0 {
		m.usageTotal += u.TotalTokens
	} else {
		m.usageTotal += u.InputTokens + u.OutputTokens
	}
}

// tokenSummaryLine shows cumulative token usage for this TUI session (below the input).
func (m *Model) tokenSummaryLine() string {
	if m.usageIn == 0 && m.usageOut == 0 && m.usageTotal == 0 {
		return "tokens: —"
	}
	return fmt.Sprintf("tokens: in %d · out %d · total %d", m.usageIn, m.usageOut, m.usageTotal)
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
		m.streamDone = &done
		unsub := m.agent.Subscribe(func(e communi.AgentEvent) {
			m.handleAgentEvent(e, &done, ch, "")
		})
		defer unsub()
		m.syncSubAgentSubscriptions(ch, &done)

		ctx := context.Background()
		if m.session != nil && strings.TrimSpace(m.session.Path) != "" {
			ctx = memory.WithSessionID(ctx, m.session.Path)
		}
		err := m.agent.Prompt(ctx, prompt)
		done.Store(true) // block further sends before closing
		m.unsubscribeAllSubAgentsForCh(ch)
		unsub()
		m.streamDone = nil
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

func (m *Model) handleAgentEvent(e communi.AgentEvent, done *atomic.Bool, ch chan streamEvent, subID string) {
	if done.Load() {
		return
	}
	if subID == "" && m.session != nil && e.Message != nil {
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
			case ch <- streamEvent{Delta: e.LlmEvent.TextDelta, Ch: ch, SubID: subID}:
			default:
			}
		}
		if e.LlmEvent.ThinkingDelta != "" {
			select {
			case ch <- streamEvent{ThinkingDelta: e.LlmEvent.ThinkingDelta, Ch: ch, SubID: subID}:
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
		case ch <- streamEvent{FinalThinking: finalThinking, FinalContent: finalContent, Ch: ch, SubID: subID}:
		default:
		}
	case communi.EventToolExecutionStart:
		lines := formatToolExecutionStartLines(e.ToolName, e.ToolCallID, e.ToolArgs)
		if len(lines) == 0 {
			return
		}
		select {
		case ch <- streamEvent{ToolText: strings.Join(lines, "\n"), Ch: ch, SubID: subID}:
		default:
		}
	case communi.EventTurnEnd:
		if e.LlmEvent != nil && e.LlmEvent.Usage != nil {
			u := *e.LlmEvent.Usage
			select {
			case ch <- streamEvent{TurnUsage: &u, Ch: ch, SubID: subID}:
			default:
			}
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
		case ch <- streamEvent{ToolText: strings.Join(lines, "\n"), Ch: ch, SubID: subID}:
		default:
		}
	}
}

func (m *Model) syncSubAgentSubscriptions(ch chan streamEvent, done *atomic.Bool) {
	sac := m.agent.SubAgentController()
	if sac == nil {
		return
	}
	chKey := fmt.Sprintf("%p", ch)
	for _, info := range sac.List() {
		key := info.SubID + "|" + chKey
		if _, exists := m.subAgentSubKeys[key]; exists {
			continue
		}
		ag := sac.AgentBySubID(info.SubID)
		if ag == nil {
			continue
		}
		subID := info.SubID
		u := ag.Subscribe(func(e communi.AgentEvent) {
			m.handleAgentEvent(e, done, ch, subID)
		})
		m.subAgentSubKeys[key] = u
	}
}

func (m *Model) unsubscribeAllSubAgentsForCh(ch chan streamEvent) {
	chKey := fmt.Sprintf("%p", ch)
	for k, unsub := range m.subAgentSubKeys {
		if !strings.HasSuffix(k, "|"+chKey) {
			continue
		}
		unsub()
		delete(m.subAgentSubKeys, k)
	}
}

func (m *Model) ensureSubPanel(subID string) *subAgentPanelState {
	if m.subAgentPanels[subID] == nil {
		m.subAgentPanels[subID] = &subAgentPanelState{}
	}
	return m.subAgentPanels[subID]
}

func (m *Model) applyStreamEventToSub(msg streamEvent) {
	p := m.ensureSubPanel(msg.SubID)
	if msg.FinalThinking != "" {
		p.history = append(p.history, "[Thinking] "+msg.FinalThinking)
		p.streamingThinking = ""
	}
	if msg.FinalContent != "" {
		p.history = append(p.history, "Assistant: "+msg.FinalContent)
		p.streamingContent = ""
	}
	if msg.ToolText != "" {
		p.history = append(p.history, strings.Split(msg.ToolText, "\n")...)
	}
	if msg.ThinkingDelta != "" {
		p.streamingThinking += msg.ThinkingDelta
	}
	if msg.Delta != "" {
		p.streamingContent += msg.Delta
	}
	if msg.Delta != "" || msg.ThinkingDelta != "" {
		delete(m.subScrollLocked, msg.SubID)
	}
}

func (m *Model) pruneSubAgentPanels() {
	sac := m.agent.SubAgentController()
	if sac == nil {
		return
	}
	listed := make(map[string]bool)
	for _, info := range sac.List() {
		listed[info.SubID] = true
	}
	for id := range m.subAgentPanels {
		if !listed[id] {
			delete(m.subAgentPanels, id)
			delete(m.subViewports, id)
			delete(m.subScrollLocked, id)
		}
	}
	for id := range m.subViewports {
		if !listed[id] {
			delete(m.subViewports, id)
			delete(m.subScrollLocked, id)
		}
	}
}

// subAgentGridView renders sub-agents in a grid (max 3 per row), each with the same framed layout
// and an independent scrollable viewport as the main agent.
func (m *Model) subAgentGridView(wrapWidth, subRegionH int) string {
	sac := m.agent.SubAgentController()
	if sac == nil {
		return ""
	}
	list := sac.List()
	if len(list) == 0 {
		return ""
	}
	m.pruneSubAgentPanels()
	n := len(list)
	const maxCols = 3
	cols := min(maxCols, n)
	rows := (n + cols - 1) / cols
	gap := 1
	cellW := (wrapWidth - (cols-1)*gap) / cols
	if cellW < 14 {
		cellW = 14
	}
	innerCellW := cellW - 4
	if innerCellW < 8 {
		innerCellW = 8
	}
	rowTotalH := (subRegionH - (rows-1)*gap) / rows
	if rowTotalH < panelFrameExtra+3 {
		rowTotalH = panelFrameExtra + 3
	}
	vpH := rowTotalH - panelFrameExtra
	if vpH < 3 {
		vpH = 3
	}

	var rowViews []string
	idx := 0
	for r := 0; r < rows; r++ {
		var cells []string
		for c := 0; c < cols && idx < n; c++ {
			info := list[idx]
			idx++
			p := m.ensureSubPanel(info.SubID)
			vp := m.ensureSubViewport(info.SubID)
			vp.Width = innerCellW
			vp.Height = vpH
			body := renderTranscriptBody(p.history, p.streamingThinking, p.streamingContent)
			vp.SetContent(lipgloss.NewStyle().Width(innerCellW).Render(body))
			if m.shouldAutoFollowSub(info.SubID) {
				vp.GotoBottom()
			}
			m.subViewports[info.SubID] = vp

			title := fmt.Sprintf("Sub: %s", info.SubID)
			cell := renderAgentPanel(cellW, title, vp.View())
			cells = append(cells, cell)
		}
		if len(cells) > 0 {
			var parts []string
			for i, cell := range cells {
				if i > 0 {
					parts = append(parts, strings.Repeat(" ", gap))
				}
				parts = append(parts, cell)
			}
			row := lipgloss.JoinHorizontal(lipgloss.Top, parts...)
			rowViews = append(rowViews, normalizeBlockWidth(row, wrapWidth))
		}
	}
	if len(rowViews) == 0 {
		return ""
	}
	return lipgloss.JoinVertical(lipgloss.Left, rowViews...)
}

func (m *Model) shouldAutoFollowSub(subID string) bool {
	if m.subScrollLocked[subID] {
		return false
	}
	p := m.subAgentPanels[subID]
	if p == nil {
		return false
	}
	return strings.TrimSpace(p.streamingContent) != "" || strings.TrimSpace(p.streamingThinking) != ""
}

func (m *Model) cycleScrollFocus() tea.Cmd {
	n := len(m.sortedSubIDs())
	if n == 0 {
		return nil
	}
	m.scrollFocus = (m.scrollFocus + 1) % (n + 1)
	return nil
}

func (m *Model) scrollFocusedViewport(lines int, up bool) {
	ids := m.sortedSubIDs()
	if m.scrollFocus == 0 {
		if up {
			m.autoFollow = false
			m.viewport.LineUp(lines)
		} else {
			m.viewport.LineDown(lines)
			if m.viewport.AtBottom() {
				m.autoFollow = true
			}
		}
		return
	}
	if m.scrollFocus < 1 || m.scrollFocus > len(ids) {
		return
	}
	sid := ids[m.scrollFocus-1]
	m.subScrollLocked[sid] = true
	vp := m.ensureSubViewport(sid)
	if up {
		vp.LineUp(lines)
	} else {
		vp.LineDown(lines)
	}
	m.subViewports[sid] = vp
}

func (m *Model) scrollFocusedGotoTop() {
	if m.scrollFocus == 0 {
		m.autoFollow = false
		m.viewport.GotoTop()
		return
	}
	ids := m.sortedSubIDs()
	if m.scrollFocus < 1 || m.scrollFocus > len(ids) {
		return
	}
	sid := ids[m.scrollFocus-1]
	m.subScrollLocked[sid] = true
	vp := m.ensureSubViewport(sid)
	vp.GotoTop()
	m.subViewports[sid] = vp
}

func (m *Model) scrollFocusedGotoBottom() {
	if m.scrollFocus == 0 {
		m.autoFollow = true
		m.viewport.GotoBottom()
		return
	}
	ids := m.sortedSubIDs()
	if m.scrollFocus < 1 || m.scrollFocus > len(ids) {
		return
	}
	sid := ids[m.scrollFocus-1]
	delete(m.subScrollLocked, sid)
	vp := m.ensureSubViewport(sid)
	vp.GotoBottom()
	m.subViewports[sid] = vp
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

	return runWithTView(model)
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
