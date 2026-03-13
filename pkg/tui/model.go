package tui

import (
	"acgo/pkg/keys"
	"acgo/pkg/llm/deepseek"
	"context"
	"fmt"
	"os"
	"sync/atomic"

	"acgo/pkg/agent"
	"acgo/pkg/config"
	"acgo/pkg/llm"
	"acgo/pkg/llm/openai"
	"acgo/pkg/log"
	"acgo/pkg/tools"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Model struct {
	agent            *agent.Agent
	textarea         textarea.Model
	history          []string
	streamingContent string // current assistant reply being streamed
	streamCh         chan streamEvent
	width            int // terminal width for wrapping
	height           int // terminal height
	err              error
}

// streamEvent is sent from the agent goroutine for each delta or done/error.
type streamEvent struct {
	Delta string
	Done  bool
	Err   error
	Ch    chan streamEvent
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

// NewModel constructs a minimal chat TUI model wired to the Agent.
func NewModel() (*Model, error) {
	settings, err := config.LoadSettings()
	if err != nil {
		return nil, err
	}
	settings.Tui.LoadAndInit()

	//注册所有provider
	for _, provider := range newProviderBySettings(settings.Tui.Agent) {
		llm.RegisterProvider(provider)
	}

	// Choose default model: DefaultModelID first, then DefaultProvider's first model, else openai.
	var m llm.Model
	if got, ok := llm.GetModel(settings.DefaultModelID); ok {
		m = got
	}
	if m.ID == "" {
		return nil, fmt.Errorf("no models available for provider")
	}

	builtinTools := []agent.AgentTool{
		tools.NewReadTool(),
		tools.NewWriteTool(),
		tools.NewBashTool(),
	}

	ag := agent.New("default", agent.Options{
		InitialState: agent.AgentState{
			SystemPrompt: "You are a helpful coding assistant.",
			Model:        m,
			Tools:        builtinTools,
		},
	})

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
	// Log is configured once in main; do not re-init here.
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
				m.history = append(m.history, "Error: "+msg.Err.Error())
			} else {
				log.Debugf("stream done content_len=%d", len(m.streamingContent))
				if m.streamingContent != "" {
					m.history = append(m.history, "Assistant: "+m.streamingContent)
				}
			}
			m.streamingContent = ""
			return m, nil
		}
		log.Debugf("stream delta len=%d", len(msg.Delta))
		m.streamingContent += msg.Delta
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
	// Wrap history to terminal width; leave a little margin for border.
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
	if m.streamingContent != "" {
		body += "Assistant: " + m.streamingContent + "▌"
	}
	if body == "" {
		body = " "
	}
	return lipgloss.JoinVertical(
		lipgloss.Left,
		historyStyle.Render(body),
		inputStyle.Render(m.textarea.View()),
	)
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
			switch e.Type {
			case agent.EventMessageUpdate:
				if e.LlmEvent != nil && e.LlmEvent.TextDelta != "" {
					select {
					case ch <- streamEvent{Delta: e.LlmEvent.TextDelta, Ch: ch}:
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

// Run launches the TUI.
func Run() error {
	model, err := NewModel()
	if err != nil {
		return err
	}
	p := tea.NewProgram(model, tea.WithOutput(os.Stdout))
	_, err = p.Run()
	return err
}
