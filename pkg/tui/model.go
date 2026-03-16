package tui

import (
	"acgo/pkg/keys"
	"acgo/pkg/llm/deepseek"
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"acgo/pkg/agent"
	"acgo/pkg/config"
	"acgo/pkg/llm"
	"acgo/pkg/llm/openai"
	"acgo/pkg/log"
	"acgo/pkg/session"
	"acgo/pkg/tools"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Model struct {
	agent             *agent.Agent
	session           *session.Session
	sessionPath       string
	textarea          textarea.Model
	history           []string
	streamingContent  string
	streamingThinking string
	streamCh          chan streamEvent
	width             int
	height            int
	err               error
}

// streamEvent is sent from the agent goroutine for each delta or done/error.
type streamEvent struct {
	Delta         string // text content delta
	ThinkingDelta string // reasoning/thinking delta (pi-ai thinking_* events)
	Done          bool
	Err           error
	Ch            chan streamEvent
}

func newProviderBySettings(settings config.TuiAgent) []llm.Provider {
	var providers []llm.Provider
	for _, providerSetting := range settings.Providers {
		switch providerSetting.Provider {
		case keys.ProviderTypeOpenAi:
			providers = append(providers, openai.NewClient(providerSetting))
		case keys.ProviderTypeDeepSeek:
			providers = append(providers, deepseek.NewClient(providerSetting))
		default:
			continue
		}
	}
	return providers
}

// ModelOptions configures NewModel (session and optional initial messages).
type ModelOptions struct {
	Session         *session.Session
	SessionPath     string
	InitialMessages []agent.AgentMessage
}

// NewModel constructs a minimal chat TUI model wired to the Agent.
// If opts.InitialMessages is set, the agent's history is replaced with them (e.g. after loading a session).
func NewModel(opts *ModelOptions) (*Model, error) {
	settings, err := config.LoadSettings()
	if err != nil {
		return nil, err
	}
	settings.Tui.LoadAndInit()

	for _, provider := range newProviderBySettings(settings.Tui.Agent) {
		llm.RegisterProvider(provider)
	}

	var m llm.Model
	if got, ok := llm.GetModel(settings.Tui.Agent.DefaultModel); ok {
		m = got
	}
	if m.ID == "" {
		return nil, fmt.Errorf("no models available for provider")
	}

	builtinTools := []agent.AgentTool{
		tools.NewReadTool(),
		tools.NewWriteTool(),
		tools.NewBashTool(),
		tools.NewEditTool(),
		tools.NewGrepTool(),
		tools.NewListTool(),
	}

	ag := agent.New("default", agent.Options{
		InitialState: agent.AgentState{
			SystemPrompt: "You are a helpful coding assistant.",
			Model:        m,
			Tools:        builtinTools,
		},
	})
	if opts != nil && len(opts.InitialMessages) > 0 {
		ag.ReplaceMessages(opts.InitialMessages)
	}

	ta := textarea.New()
	ta.Placeholder = "Ask acgo about your thoughts..."
	ta.SetHeight(1)
	ta.ShowLineNumbers = false
	ta.Focus()

	model := &Model{
		agent:    ag,
		textarea: ta,
		history:  nil,
		width:    80,
		height:   24,
	}
	if opts != nil {
		model.session = opts.Session
		model.sessionPath = opts.SessionPath
	}
	return model, nil
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
		switch msg.String() {
		case "ctrl+c", "esc":
			return m, tea.Quit
		case "enter":
			if m.textarea.Focused() {
				input := m.textarea.Value()
				m.textarea.SetValue("")
				if input == "" {
					return m, nil
				}
				log.Debugf("send prompt len=%d", len(input))
				m.history = append(m.history, "You: "+input)
				return m, m.runAgentStream(input)
			}
		}
	case streamEvent:
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
		if msg.ThinkingDelta != "" {
			m.streamingThinking += msg.ThinkingDelta
		}
		if msg.Delta != "" {
			log.Debugf("stream delta len=%d", len(msg.Delta))
			m.streamingContent += msg.Delta
		}
		// Schedule next read from the same channel.
		return m, m.waitForStreamEvent(msg.Ch)
	}

	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	return m, cmd
}

func (m Model) View() string {
	w := m.width
	if w <= 0 {
		w = 80
	}
	wrapWidth := w - 2
	if wrapWidth < 20 {
		wrapWidth = 20
	}
	historyStyle := lipgloss.NewStyle().Border(lipgloss.NormalBorder()).Height(10).Width(wrapWidth)
	inputStyle := lipgloss.NewStyle().Border(lipgloss.NormalBorder())

	body := ""
	for _, line := range m.history {
		body += line + "\n"
	}
	if m.streamingThinking != "" {
		body += "[Thinking] " + m.streamingThinking + "▌\n"
	}
	if m.streamingContent != "" {
		body += "Assistant: " + m.streamingContent + "▌"
	}
	if body == "" {
		body = " "
	}
	views := []string{}
	if m.sessionPath != "" {
		views = append(views, lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("Session: "+m.sessionPath))
	}
	views = append(views, historyStyle.Render(body), inputStyle.Render(m.textarea.View()))
	return lipgloss.JoinVertical(lipgloss.Left, views...)
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
			}
		})
		defer unsub()

		ctx := context.Background()
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

// Run launches the TUI. If sessionPath is empty, a new session file is created under config session root.
// If sessionPath is set, that file is opened and messages are loaded into the agent.
func Run(sessionPath string) error {
	settings, err := config.LoadSettings()
	if err != nil {
		return err
	}
	root := settings.Session.Root
	if root == "" {
		home, _ := os.UserHomeDir()
		root = home + "/.acgo/sessions"
	}

	var sess *session.Session
	var path string
	var initial []agent.AgentMessage

	if sessionPath == "" {
		path, err = session.NewSessionPath(root)
		if err != nil {
			return fmt.Errorf("create session path: %w", err)
		}
		sess, err = session.Create(path)
		if err != nil {
			return fmt.Errorf("create session: %w", err)
		}
	} else {
		path = sessionPath
		sess = session.Open(path)
		msgs, err := sess.LoadAll()
		if err == nil && len(msgs) > 0 {
			initial = sessionMessagesToAgent(msgs)
		}
	}

	model, err := NewModel(&ModelOptions{Session: sess, SessionPath: path, InitialMessages: initial})
	if err != nil {
		return err
	}
	p := tea.NewProgram(model, tea.WithOutput(os.Stdout))
	_, err = p.Run()
	return err
}

func sessionMessagesToAgent(msgs []session.Message) []agent.AgentMessage {
	out := make([]agent.AgentMessage, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, agent.AgentMessage{
			ID:       m.ID,
			Role:     agent.AgentMessageRole(m.Role),
			Content:  m.Content,
			Metadata: m.Metadata,
		})
	}
	return out
}

func agentMessageToSession(msg *agent.AgentMessage) session.Message {
	return session.Message{
		ID:        msg.ID,
		Role:      string(msg.Role),
		Content:   msg.Content,
		CreatedAt: time.Now().UTC(),
		Metadata:  msg.Metadata,
	}
}
