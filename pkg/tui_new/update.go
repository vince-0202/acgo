package tui_new

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/vince-0202/acgo/pkg/errors"
)

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
			input := strings.TrimSpace(m.textarea.Value())
			if input == "" || st.IsStreaming {
				break
			}
			if strings.HasPrefix(input, "/") {
				reply, quit := m.runCommand(input)
				m.textarea.SetValue("")
				if strings.TrimSpace(reply) != "" {
					m.history = append(m.history, "> "+reply)
				}
				m.autoFollow = true
				if quit {
					return m, tea.Quit
				}
				return m, nil
			}
			m.textarea.SetValue("")
			m.history = append(m.history, "You: "+input)
			m.autoFollow = true
			return m, m.runAgentStream(input)
		case "pgup":
			m.autoFollow = false
			m.viewport.LineUp(10)
			return m, nil
		case "pgdown":
			m.viewport.LineDown(10)
			if m.viewport.AtBottom() {
				m.autoFollow = true
			}
			return m, nil
		case "up":
			m.autoFollow = false
			m.viewport.LineUp(1)
			return m, nil
		case "down":
			m.viewport.LineDown(1)
			if m.viewport.AtBottom() {
				m.autoFollow = true
			}
			return m, nil
		case "home":
			m.autoFollow = false
			m.viewport.GotoTop()
			return m, nil
		case "end":
			m.autoFollow = true
			m.viewport.GotoBottom()
			return m, nil
		}
	case tea.MouseMsg:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.autoFollow = false
			m.viewport.LineUp(3)
			return m, nil
		case tea.MouseButtonWheelDown:
			m.viewport.LineDown(3)
			if m.viewport.AtBottom() {
				m.autoFollow = true
			}
			return m, nil
		}
	case streamEvent:
		if msg.TurnUsage != nil {
			m.accumulateSessionUsage(msg.TurnUsage)
		}
		if msg.SubID != "" {
			m.applyStreamEventToSub(msg)
			return m, m.waitForStreamEvent(msg.Ch)
		}
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
				if text := strings.TrimSpace(errors.FormatErrorForDisplay(msg.Err)); text != "" {
					m.err = msg.Err
					m.history = append(m.history, "Error: "+text)
					m.autoFollow = true
				}
			} else {
				if m.streamingThinking != "" {
					m.history = append(m.history, "[Thinking] "+m.streamingThinking)
				}
				if m.streamingContent != "" {
					m.history = append(m.history, "Assistant: "+m.streamingContent)
				}
				m.autoFollow = true
			}
			m.streamingThinking = ""
			m.streamingContent = ""
			return m, nil
		}
		if msg.ToolLine != nil {
			m.applyToolLine(msg.ToolLine)
			m.autoFollow = true
		}
		if msg.ThinkingDelta != "" {
			m.streamingThinking += msg.ThinkingDelta
		}
		if msg.Delta != "" {
			m.streamingContent += msg.Delta
		}
		return m, m.waitForStreamEvent(msg.Ch)
	}

	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	return m, cmd
}
