package tui_new

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m *Model) View() string {
	w := m.width
	if w <= 0 {
		w = 80
	}
	if liveW := detectTerminalWidth(w); liveW > w {
		w = liveW
	}
	wrapWidth := w - 1
	if wrapWidth < 20 {
		wrapWidth = 20
	}

	m.pruneSubAgentPanels()
	mainBody := renderTranscriptBody(m.history, m.streamingThinking, m.streamingContent)
	var commandView string
	if v := strings.TrimSpace(m.textarea.Value()); strings.HasPrefix(v, "/") {
		commandView = m.commandPaletteView()
	}

	h := m.height
	if h <= 0 {
		h = 24
	}

	statusStyle := lipgloss.NewStyle().Width(wrapWidth).Foreground(lipgloss.Color("240"))
	statusStr := statusStyle.Render(truncateToWidth(m.statusLine(), wrapWidth))
	statusLines := strings.Count(statusStr, "\n") + 1

	inputStr := lipgloss.NewStyle().Border(lipgloss.DoubleBorder(), true, false, true, false).Render(m.textarea.View())
	inputLines := strings.Count(inputStr, "\n") + 1

	tokenStr := lipgloss.NewStyle().
		Foreground(lipgloss.Color("242")).
		Faint(true).
		Render(m.tokenSummaryLine())
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

	innerW := wrapWidth - 4
	if innerW < 16 {
		innerW = wrapWidth
	}

	nSub := len(m.agent.SubAgentManager().List())
	m.viewport.Width = innerW
	if nSub == 0 {
		m.viewport.Height = max(3, vpHeight-3)
		m.viewport.SetContent(lipgloss.NewStyle().Width(innerW).Render(mainBody))
		if m.autoFollow {
			m.viewport.GotoBottom()
		}
		mainBox := normalizeBlockWidth(renderAgentPanel(wrapWidth, " Main agent", m.viewport.View()), wrapWidth)
		views := []string{statusStr, mainBox, inputStr, tokenStr}
		if commandView != "" {
			views = append(views, commandView)
		}
		return lipgloss.JoinVertical(lipgloss.Left, views...)
	}

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

	views := []string{statusStr, mainBox, sep, grid, inputStr, tokenStr}
	if commandView != "" {
		views = append(views, commandView)
	}
	return lipgloss.JoinVertical(lipgloss.Left, views...)
}

func renderAgentPanel(totalWidth int, title string, bodyView string) string {
	if totalWidth < 8 {
		totalWidth = 8
	}
	contentWidth := totalWidth - 4
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
	for _, line := range strings.Split(body, "\n") {
		lines = append(lines, fit(line))
	}
	if len(lines) == 1 {
		lines = append(lines, strings.Repeat(" ", contentWidth))
	}

	var b strings.Builder
	b.WriteString("┌" + strings.Repeat("─", totalWidth-2) + "┐\n")
	for _, line := range lines {
		b.WriteString("│ " + line + " │\n")
	}
	b.WriteString("└" + strings.Repeat("─", totalWidth-2) + "┘")
	return b.String()
}

func normalizeBlockWidth(block string, width int) string {
	if width <= 0 {
		return ""
	}
	lines := strings.Split(block, "\n")
	for i, line := range lines {
		line = clipToWidth(line, width)
		w := lipgloss.Width(line)
		if w < width {
			line += strings.Repeat(" ", width-w)
		}
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}

func renderTranscriptBody(history []string, streamThink, streamContent string) string {
	var b strings.Builder
	for _, line := range history {
		b.WriteString(stripANSI(line))
		b.WriteString("\n")
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
