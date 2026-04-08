package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/vince-0202/acgo/pkg/communi"
	"github.com/vince-0202/acgo/pkg/errors"
	"github.com/vince-0202/acgo/pkg/harness"
	"github.com/vince-0202/acgo/pkg/keys"
	"github.com/vince-0202/acgo/pkg/log"
	"github.com/vince-0202/acgo/pkg/memory"
)

func tviewStyleLine(line string) string {
	switch {
	case strings.HasPrefix(line, "[Thinking]"):
		content := strings.TrimSpace(strings.TrimPrefix(line, "[Thinking]"))
		return "[#d0d0d0]" + tview.Escape("Thinking > "+content) + "[-]"
	default:
		return tview.Escape(line)
	}
}

func tviewTranscriptBody(history []string, streamThink, streamContent string) string {
	var b strings.Builder
	for _, line := range history {
		b.WriteString(tviewStyleLine(stripANSI(line)) + "\n")
	}
	if streamThink != "" {
		b.WriteString("[#d0d0d0]" + tview.Escape("Thinking > "+stripANSI(streamThink)+"▌") + "[-]\n")
	}
	if streamContent != "" {
		b.WriteString(tview.Escape("Assistant: " + stripANSI(streamContent) + "▌"))
	}
	s := b.String()
	if strings.TrimSpace(s) == "" {
		return " "
	}
	return s
}

func gopherASCIIArt() string {
	return strings.Join([]string{
		"   _    ____ ____  ___ ",
		"  / \\  / ___/ ___|/ _ \\",
		" / _ \\| |  | |  _| | | |",
		"/ ___ \\ |__| |_| | |_| |",
		"/_/   \\_\\____\\____|\\___/ ",
		"",
		"   ACGO standby",
		"",
		"Shortcuts:",
		"  Shift+Tab  permission mode",
		"  Ctrl+S     steering",
		"  Ctrl+F     follow-up",
	}, "\n")
}

func compactStatusLine(statusLine string) string {
	statusLine = strings.TrimSpace(statusLine)
	parts := strings.SplitN(statusLine, " | ", 3)
	if len(parts) >= 3 && strings.HasPrefix(parts[0], "Session: ") {
		statusLine = strings.TrimSpace(parts[1] + " | " + parts[2])
	}
	parts = strings.Split(statusLine, " | ")
	if len(parts) >= 2 && strings.Contains(parts[0], "/") {
		return strings.TrimSpace(strings.Join(parts[1:], " | "))
	}
	return statusLine
}

func rightStatusPanel(statusLine, tokenLine string) string {
	var b strings.Builder
	b.WriteString("Token usage\n")
	b.WriteString("  " + strings.TrimSpace(tokenLine) + "\n\n")
	b.WriteString("Status\n")
	b.WriteString("  " + compactStatusLine(statusLine) + "\n\n")
	b.WriteString("Monitoring\n")
	b.WriteString("  - latency: --\n")
	b.WriteString("  - tools: --\n")
	b.WriteString("  - queue: --")
	return b.String()
}

func tokenSummaryLineFromUsage(in, out, total int) string {
	if in == 0 && out == 0 && total == 0 {
		return "—"
	}
	return fmt.Sprintf("in %d · out %d · total %d", in, out, total)
}

func animateTowards(current, target int) int {
	if current == target {
		return current
	}
	diff := target - current
	step := diff / 4
	if step == 0 {
		if diff > 0 {
			step = 1
		} else {
			step = -1
		}
	}
	next := current + step
	if diff > 0 && next > target {
		return target
	}
	if diff < 0 && next < target {
		return target
	}
	return next
}

func runWithTView(m *Model) error {
	app := tview.NewApplication()
	// Wheel scrolling must be handled here; with mouse reporting off, the shell scrolls scrollback instead of the transcript.
	app.EnableMouse(true)
	// Use a light gray theme instead of the default dark background.
	tview.Styles.PrimitiveBackgroundColor = tcell.GetColor("#f2f2f2")
	tview.Styles.ContrastBackgroundColor = tcell.GetColor("#e6e6e6")
	tview.Styles.MoreContrastBackgroundColor = tcell.GetColor("#dcdcdc")
	tview.Styles.BorderColor = tcell.GetColor("#666666")
	tview.Styles.TitleColor = tcell.GetColor("#222222")
	tview.Styles.PrimaryTextColor = tcell.GetColor("#222222")
	tview.Styles.SecondaryTextColor = tcell.GetColor("#444444")
	tview.Styles.TertiaryTextColor = tcell.GetColor("#666666")

	var mu sync.Mutex
	mainStreaming := false
	subStreaming := map[string]bool{}

	transcript := tview.NewTextView().SetDynamicColors(true).SetWrap(true).SetScrollable(true)
	transcript.SetBorder(true).SetTitle(" Main agent")
	leftPanel := tview.NewTextView().SetDynamicColors(false).SetWrap(true)
	leftPanel.SetBorder(true).SetTitle(" " + m.agent.Provider.Name() + "/" + m.agent.Model.ID)
	rightPanel := tview.NewTextView().SetDynamicColors(false).SetWrap(true)
	rightPanel.SetBorder(true).SetTitle(" Runtime")
	var root *tview.Flex
	panelContainer := tview.NewFlex().SetDirection(tview.FlexRow)
	mainPanel := tview.NewFlex().SetDirection(tview.FlexRow)
	subViews := map[string]*tview.TextView{}
	subInputs := map[string]*tview.InputField{}
	subPanels := map[string]*tview.Flex{}
	subInputBound := map[string]bool{}
	focusKeys := []string{"main"}
	activeKey := "main"
	permissionAwaiting := false
	permissionPrompt := ""
	permissionOptions := []string{"Allow", "Allow Always (session)", "Deny"}
	permissionSelected := 0
	permissionAllowSession := map[string]bool{}
	permissionQueueWaiting := 0
	var permissionPromptMu sync.Mutex
	var permissionDecisionCh chan string
	displayUsageIn := 0
	displayUsageOut := 0
	displayUsageTotal := 0
	targetUsageIn := 0
	targetUsageOut := 0
	targetUsageTotal := 0

	advanceUsageDisplay := func() bool {
		nextIn := animateTowards(displayUsageIn, targetUsageIn)
		nextOut := animateTowards(displayUsageOut, targetUsageOut)
		nextTotal := animateTowards(displayUsageTotal, targetUsageTotal)
		changed := nextIn != displayUsageIn || nextOut != displayUsageOut || nextTotal != displayUsageTotal
		displayUsageIn = nextIn
		displayUsageOut = nextOut
		displayUsageTotal = nextTotal
		return changed
	}

	mainInput := tview.NewInputField()
	mainInput.SetLabel("> ")
	mainInput.SetBorder(true).SetTitle(" Input")
	mainPanel.AddItem(transcript, 0, 4, false)
	mainPanel.AddItem(mainInput, 3, 0, true)

	ensureSubPanel := func(subID string) (*tview.TextView, *tview.InputField, *tview.Flex) {
		if tv, ok := subViews[subID]; ok {
			return tv, subInputs[subID], subPanels[subID]
		}
		tv := tview.NewTextView().SetDynamicColors(true).SetWrap(true).SetScrollable(true)
		tv.SetBorder(true).SetTitle(" " + subID)
		in := tview.NewInputField().SetLabel("> ")
		in.SetBorder(true).SetTitle(" Input")
		panel := tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(tv, 0, 4, false).
			AddItem(in, 3, 0, false)
		subViews[subID] = tv
		subInputs[subID] = in
		subPanels[subID] = panel
		panelContainer.AddItem(panel, 0, 3, false)
		return tv, in, panel
	}

	applyFocusStyle := func() {
		mainFocused := activeKey == "main"
		if mainFocused {
			transcript.SetTitle(" [::b]Main agent[::-]")
			transcript.SetBorderColor(tcell.ColorYellow)
			mainInput.SetBorderColor(tcell.ColorYellow)
		} else {
			transcript.SetTitle(" Main agent")
			transcript.SetBorderColor(tcell.ColorWhite)
			mainInput.SetBorderColor(tcell.ColorWhite)
		}
		for sid, tv := range subViews {
			in := subInputs[sid]
			if sid == activeKey {
				tv.SetTitle(" [::b]" + sid + "[::-]")
				tv.SetBorderColor(tcell.ColorYellow)
				in.SetBorderColor(tcell.ColorYellow)
			} else {
				tv.SetTitle(" " + sid)
				tv.SetBorderColor(tcell.ColorWhite)
				in.SetBorderColor(tcell.ColorWhite)
			}
		}
	}

	setActive := func(key string) {
		activeKey = key
		if key == "main" {
			app.SetFocus(mainInput)
		} else if in, ok := subInputs[key]; ok {
			app.SetFocus(in)
		}
		applyFocusStyle()
	}

	focusedView := func() *tview.TextView {
		if activeKey == "main" {
			return transcript
		}
		tv, _, _ := ensureSubPanel(activeKey)
		return tv
	}

	clampOffset := func(off int) int {
		if off < 0 {
			return 0
		}
		return off
	}

	scrollTextView := func(tv *tview.TextView, delta int) {
		if tv == nil {
			return
		}
		prev, _ := tv.GetScrollOffset()
		next := clampOffset(prev + delta)
		tv.ScrollTo(next, 0)
	}

	scrollFocused := func(delta int) {
		scrollTextView(focusedView(), delta)
	}

	abortRunningSubAgent := func(subID string) bool {
		if strings.TrimSpace(subID) == "" {
			return false
		}
		sac := m.agent.SubAgentController()
		if sac == nil {
			return false
		}
		ag := sac.AgentBySubID(subID)
		if ag == nil {
			return false
		}
		st := ag.State()
		if !st.IsStreaming && !subStreaming[subID] {
			return false
		}
		ag.Abort()
		return true
	}

	abortAllRunningSubAgents := func() int {
		sac := m.agent.SubAgentController()
		if sac == nil {
			return 0
		}
		aborted := 0
		for _, info := range sac.List() {
			sid := info.SubID
			ag := sac.AgentBySubID(sid)
			if ag == nil {
				continue
			}
			st := ag.State()
			if !st.IsStreaming && !subStreaming[sid] {
				continue
			}
			ag.Abort()
			aborted++
		}
		return aborted
	}

	abortActiveConversation := func() bool {
		aborted := false
		if activeKey == "main" {
			st := m.agent.State()
			if st.IsStreaming || mainStreaming {
				m.agent.Abort()
				aborted = true
			}
			if n := abortAllRunningSubAgents(); n > 0 {
				aborted = true
			}
			return aborted
		}
		if abortRunningSubAgent(activeKey) {
			return true
		}
		return false
	}

	refreshSubPanels := func() {
		m.pruneSubAgentPanels()
		ids := m.sortedSubIDs()
		sort.Strings(ids)
		focusKeys = []string{"main"}
		focusKeys = append(focusKeys, ids...)
		if activeKey != "main" {
			found := false
			for _, k := range focusKeys {
				if k == activeKey {
					found = true
					break
				}
			}
			if !found {
				activeKey = "main"
				app.SetFocus(mainInput)
			}
		}
		for _, sid := range ids {
			panel := m.ensureSubPanel(sid)
			tv, in, sp := ensureSubPanel(sid)
			_ = in
			_ = sp
			tv.SetText(tviewTranscriptBody(panel.history, panel.streamingThinking, panel.streamingContent))
			// Always follow latest content on refresh.
			tv.ScrollToEnd()
		}
		for sid := range subViews {
			keep := false
			for _, live := range ids {
				if sid == live {
					keep = true
					break
				}
			}
			if keep {
				continue
			}
			panelContainer.RemoveItem(subPanels[sid])
			delete(subViews, sid)
			delete(subInputs, sid)
			delete(subPanels, sid)
			delete(subInputBound, sid)
			delete(subStreaming, sid)
		}
		applyFocusStyle()
	}

	renderPermissionSelection := func() string {
		if !permissionAwaiting {
			return ""
		}
		var b strings.Builder
		if permissionQueueWaiting > 1 {
			b.WriteString(fmt.Sprintf("Permission required (1/%d)  (↑/↓ select, Enter confirm, Esc deny)\n", permissionQueueWaiting))
		} else {
			b.WriteString("Permission required  (↑/↓ select, Enter confirm, Esc deny)\n")
		}
		for i, opt := range permissionOptions {
			prefix := "  "
			if i == permissionSelected {
				prefix = "> "
			}
			b.WriteString(prefix + opt + "\n")
		}
		if strings.TrimSpace(permissionPrompt) != "" {
			b.WriteString("\n")
			b.WriteString(permissionPrompt + "\n")
		}
		return strings.TrimSuffix(b.String(), "\n")
	}

	updateHelpText := func() {
		if permissionAwaiting {
			leftPanel.SetText(renderPermissionSelection())
			return
		}
		text := strings.TrimSpace(mainInput.GetText())
		if strings.HasPrefix(text, "/") {
			palette := strings.TrimSpace(commandPaletteFromText(m, text))
			if palette == "" {
				leftPanel.SetText(gopherASCIIArt())
				return
			}
			leftPanel.SetText(palette)
			return
		}
		leftPanel.SetText(gopherASCIIArt())
	}

	refresh := func() {
		body := tviewTranscriptBody(m.history, m.streamingThinking, m.streamingContent)
		transcript.SetText(body)
		// Always follow latest content on refresh.
		transcript.ScrollToEnd()
		leftPanel.SetTitle(" " + m.agent.Provider.Name() + "/" + m.agent.Model.ID)
		targetUsageIn = m.usageIn
		targetUsageOut = m.usageOut
		targetUsageTotal = m.usageTotal
		advanceUsageDisplay()
		rightPanel.SetText(rightStatusPanel(
			m.statusLine(),
			tokenSummaryLineFromUsage(displayUsageIn, displayUsageOut, displayUsageTotal),
		))
		refreshSubPanels()
		updateHelpText()
	}
	refresh()

	startMainStream := func(prompt string) {
		mainStreaming = true
		ch := make(chan streamEvent, 64)
		var done atomic.Bool

		go func() {
			mu.Lock()
			m.streamDone = &done
			mu.Unlock()

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
			done.Store(true)
			m.unsubscribeAllSubAgentsForCh(ch)

			mu.Lock()
			m.streamDone = nil
			mu.Unlock()

			ch <- streamEvent{Done: true, Err: err, Ch: ch}
			close(ch)
		}()

		go func() {
			for ev := range ch {
				app.QueueUpdateDraw(func() {
					mu.Lock()
					defer mu.Unlock()
					// New sub-agents can be created during this main turn (via sub_agent tool create).
					// Keep syncing subscriptions so their subsequent task events can stream to sub panels.
					m.syncSubAgentSubscriptions(ch, &done)

					if ev.TurnUsage != nil {
						m.accumulateSessionUsage(ev.TurnUsage)
					}
					if ev.SubID != "" {
						m.applyStreamEventToSub(ev)
						refresh()
						return
					}

					if ev.FinalThinking != "" {
						m.history = append(m.history, "[Thinking] "+ev.FinalThinking)
						m.streamingThinking = ""
					}
					if ev.FinalContent != "" {
						m.history = append(m.history, "Assistant: "+ev.FinalContent)
						m.streamingContent = ""
					}
					if ev.ToolLine != nil {
						m.applyToolLineStream(ev.SubID, ev.ToolLine)
					}
					if ev.ThinkingDelta != "" {
						m.streamingThinking += ev.ThinkingDelta
					}
					if ev.Delta != "" {
						m.streamingContent += ev.Delta
					}
					if ev.Done || ev.Err != nil {
						if ev.Err != nil {
							m.err = ev.Err
							m.history = append(m.history, "Error: "+errors.FormatErrorForDisplay(ev.Err))
						} else {
							if m.streamingThinking != "" {
								m.history = append(m.history, "[Thinking] "+m.streamingThinking)
							}
							if m.streamingContent != "" {
								m.history = append(m.history, "Assistant: "+m.streamingContent)
							}
						}
						m.streamingThinking = ""
						m.streamingContent = ""
						mainStreaming = false
					}
					refresh()
				})
			}
		}()
	}

	startSubStream := func(subID string, prompt string) {
		subStreaming[subID] = true
		ch := make(chan streamEvent, 64)
		var done atomic.Bool
		go func() {
			sac := m.agent.SubAgentController()
			if sac == nil {
				ch <- streamEvent{Done: true, Err: fmt.Errorf("sub-agent controller unavailable"), Ch: ch, SubID: subID}
				close(ch)
				return
			}
			ag := sac.AgentBySubID(subID)
			if ag == nil {
				ch <- streamEvent{Done: true, Err: fmt.Errorf("unknown sub-agent: %s", subID), Ch: ch, SubID: subID}
				close(ch)
				return
			}
			var sawAssistantText atomic.Bool
			unsub := ag.Subscribe(func(e communi.AgentEvent) {
				if e.Type == communi.EventMessageUpdate && e.LlmEvent != nil &&
					(strings.TrimSpace(e.LlmEvent.TextDelta) != "" || strings.TrimSpace(e.LlmEvent.ThinkingDelta) != "") {
					sawAssistantText.Store(true)
				}
				if e.Type == communi.EventMessageEnd && e.Message != nil &&
					(strings.TrimSpace(e.Message.ContentBlocksToText()) != "" || strings.TrimSpace(e.Message.Thinking) != "") {
					sawAssistantText.Store(true)
				}
				m.handleAgentEvent(e, &done, ch, subID)
			})
			defer unsub()
			err := ag.Prompt(context.Background(), prompt)
			if err == nil && !sawAssistantText.Load() {
				msgs := ag.GetMessages()
				for i := len(msgs) - 1; i >= 0; i-- {
					if msgs[i].Role != keys.AgentRoleAssistant {
						continue
					}
					fc := strings.TrimSpace(msgs[i].ContentBlocksToText())
					ft := strings.TrimSpace(msgs[i].Thinking)
					if fc != "" || ft != "" {
						ch <- streamEvent{FinalThinking: ft, FinalContent: fc, Ch: ch, SubID: subID}
					}
					break
				}
			}
			done.Store(true)
			ch <- streamEvent{Done: true, Err: err, Ch: ch, SubID: subID}
			close(ch)
		}()
		go func() {
			for ev := range ch {
				app.QueueUpdateDraw(func() {
					mu.Lock()
					defer mu.Unlock()
					if ev.TurnUsage != nil {
						m.accumulateSessionUsage(ev.TurnUsage)
					}
					m.applyStreamEventToSub(ev)
					if ev.Done || ev.Err != nil {
						if ev.Err != nil {
							p := m.ensureSubPanel(subID)
							p.history = append(p.history, "Error: "+errors.FormatErrorForDisplay(ev.Err))
						}
						subStreaming[subID] = false
					}
					refresh()
				})
			}
		}()
	}

	trySendMain := func(raw string, mode string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		st := m.agent.State()
		if st.IsStreaming || mainStreaming {
			msg := communi.NewUserMessageWithoutId(raw)
			switch mode {
			case "followup":
				m.agent.EnqueueFollowUp(msg)
				m.history = append(m.history, "You: (follow-up) "+raw)
			default:
				m.agent.EnqueueSteering(msg)
				m.history = append(m.history, "You: (steering) "+raw)
			}
			mainInput.SetText("")
			refresh()
			return
		}

		if strings.HasPrefix(raw, "/") {
			reply, quit := m.runCommand(raw)
			m.history = append(m.history, "> "+reply)
			mainInput.SetText("")
			refresh()
			if quit {
				app.Stop()
			}
			return
		}

		if m.pendingSkillContent != "" {
			raw = m.pendingSkillContent + "\n\n---\n\n" + raw
			m.pendingSkillContent = ""
		}
		m.history = append(m.history, "You: "+raw)
		mainInput.SetText("")
		refresh()
		log.Debugf("send prompt len=%d", len(raw))
		startMainStream(raw)
	}

	trySendSub := func(subID, raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		if subStreaming[subID] {
			p := m.ensureSubPanel(subID)
			p.history = append(p.history, "Error: sub-agent is busy, wait for current response")
			refresh()
			return
		}
		p := m.ensureSubPanel(subID)
		p.history = append(p.history, "You: "+raw)
		if in, ok := subInputs[subID]; ok {
			in.SetText("")
		}
		refresh()
		log.Debugf("send sub prompt sub_id=%s len=%d", subID, len(raw))
		startSubStream(subID, raw)
	}

	mainInput.SetDoneFunc(func(_ tcell.Key) {
		mu.Lock()
		defer mu.Unlock()
		trySendMain(mainInput.GetText(), "steering")
	})

	mainInput.SetChangedFunc(func(text string) {
		_ = text
		updateHelpText()
	})

	panelContainer.AddItem(mainPanel, 0, 3, true)
	bottomPanel := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(leftPanel, 0, 1, false).
		AddItem(rightPanel, 42, 0, false)
	root = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(panelContainer, 0, 1, true).
		AddItem(bottomPanel, 11, 0, false)

	m.agent.SetPermissionConfirmHook(func(ctx context.Context, req harness.PermissionRequest) (bool, string, error) {
		mu.Lock()
		permissionQueueWaiting++
		mu.Unlock()
		app.QueueUpdateDraw(func() {
			mu.Lock()
			if permissionAwaiting {
				updateHelpText()
			}
			mu.Unlock()
		})
		defer func() {
			mu.Lock()
			if permissionQueueWaiting > 0 {
				permissionQueueWaiting--
			}
			mu.Unlock()
			app.QueueUpdateDraw(func() {
				mu.Lock()
				if permissionAwaiting {
					updateHelpText()
				}
				mu.Unlock()
			})
		}()

		// Serialize all permission prompts (main/sub agents) to avoid channel overwrite
		// and stuck confirmations when multiple tool calls ask concurrently.
		permissionPromptMu.Lock()
		defer permissionPromptMu.Unlock()

		permissionKey := strings.TrimSpace(req.Action) + "|" + strings.TrimSpace(req.Resource)
		mu.Lock()
		alwaysAllowed := permissionAllowSession[permissionKey]
		mu.Unlock()
		if alwaysAllowed {
			return true, "approved by tui user (session remember)", nil
		}

		toolName, _ := req.Metadata["tool_name"].(string)
		toolCallID, _ := req.Metadata["tool_call_id"].(string)
		toolArgs, _ := req.Metadata["tool_args"].(string)
		if strings.TrimSpace(toolName) == "" {
			toolName = req.Resource
		}

		var text strings.Builder
		text.WriteString("Tool permission request\n\n")
		text.WriteString(formatToolGTLine(toolName, []byte(toolArgs)) + "\n")
		if strings.TrimSpace(toolCallID) != "" {
			text.WriteString("Call ID: " + toolCallID + "\n")
		}
		text.WriteString("Action: " + req.Action + "\n")
		text.WriteString("Resource: " + req.Resource + "\n")
		if strings.TrimSpace(toolArgs) != "" {
			if len(toolArgs) > 600 {
				toolArgs = toolArgs[:600] + "..."
			}
			text.WriteString("\nArguments (JSON):\n" + toolArgs + "\n")
		}
		text.WriteString("\nAllow this tool call?")

		decisionCh := make(chan string, 1)
		mu.Lock()
		permissionDecisionCh = decisionCh
		mu.Unlock()
		app.QueueUpdateDraw(func() {
			mu.Lock()
			permissionAwaiting = true
			permissionPrompt = strings.TrimSpace(text.String())
			permissionSelected = 0
			updateHelpText()
			mu.Unlock()
		})
		defer app.QueueUpdateDraw(func() {
			mu.Lock()
			permissionAwaiting = false
			permissionPrompt = ""
			permissionSelected = 0
			permissionDecisionCh = nil
			updateHelpText()
			mu.Unlock()
		})

		select {
		case decision := <-decisionCh:
			switch decision {
			case "allow_always":
				mu.Lock()
				permissionAllowSession[permissionKey] = true
				mu.Unlock()
				return true, "approved by tui user (remembered for this session)", nil
			case "allow_once":
				return true, "approved by tui user", nil
			default:
				return false, "rejected by tui user", nil
			}
		case <-ctx.Done():
			return false, "permission confirmation canceled", ctx.Err()
		}
	})

	app.SetRoot(root, true)
	app.SetFocus(mainInput)
	applyFocusStyle()
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		mu.Lock()
		defer mu.Unlock()

		if permissionAwaiting {
			switch event.Key() {
			case tcell.KeyUp:
				if permissionSelected > 0 {
					permissionSelected--
				}
				updateHelpText()
				return nil
			case tcell.KeyDown:
				if permissionSelected < len(permissionOptions)-1 {
					permissionSelected++
				}
				updateHelpText()
				return nil
			case tcell.KeyEnter:
				decision := "deny"
				switch permissionSelected {
				case 0:
					decision = "allow_once"
				case 1:
					decision = "allow_always"
				}
				if permissionDecisionCh != nil {
					select {
					case permissionDecisionCh <- decision:
					default:
					}
				}
				return nil
			case tcell.KeyEscape:
				if permissionDecisionCh != nil {
					select {
					case permissionDecisionCh <- "deny":
					default:
					}
				}
				return nil
			}
			return nil
		}

		switch event.Key() {
		case tcell.KeyBacktab:
			label, err := m.cyclePermissionMode()
			if err != nil {
				m.history = append(m.history, "Error: switch permission mode failed: "+err.Error())
			} else {
				m.history = append(m.history, "System: permission mode -> "+label)
			}
			refresh()
			return nil
		case tcell.KeyCtrlC:
			if abortActiveConversation() {
				refresh()
				return nil
			}
			app.Stop()
			return nil
		case tcell.KeyEscape:
			if abortActiveConversation() {
				refresh()
				return nil
			}
			app.Stop()
			return nil
		case tcell.KeyCtrlS:
			if activeKey == "main" {
				trySendMain(mainInput.GetText(), "steering")
			}
			return nil
		case tcell.KeyCtrlF:
			if activeKey == "main" {
				trySendMain(mainInput.GetText(), "followup")
			}
			return nil
		}
		return event
	})
	app.SetMouseCapture(func(event *tcell.EventMouse, action tview.MouseAction) (*tcell.EventMouse, tview.MouseAction) {
		mu.Lock()
		defer mu.Unlock()
		x, y := event.Position()
		inRect := func(p tview.Primitive) bool {
			if p == nil {
				return false
			}
			rx, ry, rw, rh := p.GetRect()
			return x >= rx && x < rx+rw && y >= ry && y < ry+rh
		}
		if action == tview.MouseLeftClick {
			if inRect(transcript) || inRect(mainInput) {
				setActive("main")
			} else {
				for sid, tv := range subViews {
					if inRect(tv) || inRect(subInputs[sid]) {
						setActive(sid)
						break
					}
				}
			}
		}
		wheelOverOutput := func(delta int) {
			if inRect(transcript) {
				scrollTextView(transcript, delta)
				return
			}
			for _, tv := range subViews {
				if inRect(tv) {
					scrollTextView(tv, delta)
					return
				}
			}
			scrollFocused(delta)
		}
		switch action {
		case tview.MouseScrollUp:
			wheelOverOutput(-3)
			return nil, tview.MouseConsumed
		case tview.MouseScrollDown:
			wheelOverOutput(3)
			return nil, tview.MouseConsumed
		}
		return event, action
	})

	// Bind per-sub-agent send handler once inputs are created.
	refreshSubPanels = func() {
		m.pruneSubAgentPanels()
		ids := m.sortedSubIDs()
		sort.Strings(ids)
		focusKeys = []string{"main"}
		focusKeys = append(focusKeys, ids...)
		if activeKey != "main" {
			found := false
			for _, k := range focusKeys {
				if k == activeKey {
					found = true
					break
				}
			}
			if !found {
				activeKey = "main"
				app.SetFocus(mainInput)
			}
		}
		for _, sid := range ids {
			panel := m.ensureSubPanel(sid)
			tv, in, _ := ensureSubPanel(sid)
			if !subInputBound[sid] {
				subID := sid
				in.SetDoneFunc(func(_ tcell.Key) {
					mu.Lock()
					defer mu.Unlock()
					trySendSub(subID, in.GetText())
				})
				subInputBound[sid] = true
			}
			tv.SetText(tviewTranscriptBody(panel.history, panel.streamingThinking, panel.streamingContent))
			// Always follow latest content on refresh.
			tv.ScrollToEnd()
		}
		for sid := range subViews {
			keep := false
			for _, live := range ids {
				if sid == live {
					keep = true
					break
				}
			}
			if keep {
				continue
			}
			panelContainer.RemoveItem(subPanels[sid])
			delete(subViews, sid)
			delete(subInputs, sid)
			delete(subPanels, sid)
			delete(subInputBound, sid)
			delete(subStreaming, sid)
		}
		applyFocusStyle()
	}
	refresh()

	stopUsageAnim := make(chan struct{})
	go func() {
		ticker := time.NewTicker(90 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopUsageAnim:
				return
			case <-ticker.C:
				app.QueueUpdateDraw(func() {
					mu.Lock()
					defer mu.Unlock()
					if !advanceUsageDisplay() {
						return
					}
					rightPanel.SetText(rightStatusPanel(
						m.statusLine(),
						tokenSummaryLineFromUsage(displayUsageIn, displayUsageOut, displayUsageTotal),
					))
				})
			}
		}
	}()

	err := app.Run()
	close(stopUsageAnim)
	return err
}

func commandPaletteFromText(m *Model, raw string) string {
	if !strings.HasPrefix(strings.TrimSpace(raw), "/") {
		return ""
	}
	query := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(raw, "/")))
	var out []string
	for _, spec := range m.commandSpecs {
		usage := spec.Usage
		if strings.TrimSpace(usage) == "" {
			usage = "/" + spec.Name
		}
		target := strings.ToLower(spec.Name + " " + strings.Join(spec.Aliases, " ") + " " + usage)
		if query != "" && !strings.Contains(target, query) {
			continue
		}
		if strings.TrimSpace(spec.Help) != "" {
			out = append(out, fmt.Sprintf("%s - %s", usage, strings.TrimSpace(spec.Help)))
		} else {
			out = append(out, usage)
		}
		if len(out) >= 8 {
			break
		}
	}
	return strings.Join(out, "\n")
}
