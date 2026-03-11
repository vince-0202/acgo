package tui

import (
	"context"
	"fmt"
	"os"

	"go-pi/pkg/agent"
	"go-pi/pkg/config"
	"go-pi/pkg/llm"
	"go-pi/pkg/llm/openai"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Model struct {
	agent   *agent.Agent
	textarea textarea.Model
	history []string
	err     error
}

// NewModel constructs a minimal chat TUI model wired to the Agent.
func NewModel() (*Model, error) {
	// Register OpenAI provider and models.
	openai.Register()

	settings, err := config.LoadSettings()
	if err != nil {
		return nil, err
	}

	// Choose default model.
	var m llm.Model
	if settings.DefaultModelID != "" {
		if got, ok := llm.GetModel(settings.DefaultModelID); ok {
			m = got
		}
	}
	if m.ID == "" {
		// fallback to first OpenAI model.
		provider, _ := llm.GetProvider("openai")
		models := provider.Models()
		if len(models) == 0 {
			return nil, fmt.Errorf("no models available for provider openai")
		}
		m = models[0]
	}

	ag := agent.New("default", agent.Options{
		InitialState: agent.AgentState{
			SystemPrompt: "You are a helpful coding assistant.",
			Model:        m,
		},
	})

	ta := textarea.New()
	ta.Placeholder = "Ask pi-go about your code..."
	ta.Focus()

	return &Model{
		agent:   ag,
		textarea: ta,
		history: nil,
	}, nil
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
				m.history = append(m.history, "You: "+input)
				return m, m.askAgent(input)
			}
		}
	case assistantReplyMsg:
		if msg.err != nil {
			m.err = msg.err
			m.history = append(m.history, "Error: "+msg.err.Error())
		} else {
			m.history = append(m.history, "Assistant: "+msg.text)
		}
	}

	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	return m, cmd
}

func (m Model) View() string {
	historyStyle := lipgloss.NewStyle().Border(lipgloss.NormalBorder()).Height(10)
	inputStyle := lipgloss.NewStyle().Border(lipgloss.NormalBorder())

	history := ""
	for _, line := range m.history {
		history += line + "\n"
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		historyStyle.Render(history),
		inputStyle.Render(m.textarea.View()),
	)
}

func (m *Model) askAgent(prompt string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		err := m.agent.Prompt(ctx, prompt)
		if err != nil {
			return assistantReplyMsg{err: err}
		}
		// Last message should be assistant reply.
		state := m.agent.State()
		if len(state.Messages) == 0 {
			return assistantReplyMsg{text: ""}
		}
		last := state.Messages[len(state.Messages)-1]
		return assistantReplyMsg{text: last.Content}
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

