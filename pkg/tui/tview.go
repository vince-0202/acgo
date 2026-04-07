package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/vince-0202/acgo/pkg/communi"
	"github.com/vince-0202/acgo/pkg/errors"
	"github.com/vince-0202/acgo/pkg/log"
	"github.com/vince-0202/acgo/pkg/memory"
)

func runWithTView(m *Model) error {
	app := tview.NewApplication()
	app.EnableMouse(true)

	var mu sync.Mutex
	mainStreaming := false
	subStreaming := map[string]bool{}

	status := tview.NewTextView().SetDynamicColors(false).SetWrap(false)
	transcript := tview.NewTextView().SetDynamicColors(false).SetWrap(true).SetScrollable(true)
	transcript.SetBorder(true).SetTitle(" Main agent")
	tokens := tview.NewTextView().SetDynamicColors(false).SetWrap(false)
	help := tview.NewTextView().SetDynamicColors(false).SetWrap(true)
	help.SetText("Click panel to focus | Enter: send | Ctrl+C/Esc: abort/quit")
	var root *tview.Flex
	panelContainer := tview.NewFlex().SetDirection(tview.FlexRow)
	mainPanel := tview.NewFlex().SetDirection(tview.FlexRow)
	subViews := map[string]*tview.TextView{}
	subInputs := map[string]*tview.InputField{}
	subPanels := map[string]*tview.Flex{}
	subInputBound := map[string]bool{}
	scrollOffset := map[string]int{}
	scrollLocked := map[string]bool{}
	focusKeys := []string{"main"}
	activeKey := "main"

	mainInput := tview.NewInputField()
	mainInput.SetLabel("> ")
	mainInput.SetBorder(true).SetTitle(" Input")
	mainPanel.AddItem(transcript, 0, 4, false)
	mainPanel.AddItem(mainInput, 3, 0, true)

	ensureSubPanel := func(subID string) (*tview.TextView, *tview.InputField, *tview.Flex) {
		if tv, ok := subViews[subID]; ok {
			return tv, subInputs[subID], subPanels[subID]
		}
		tv := tview.NewTextView().SetDynamicColors(false).SetWrap(true).SetScrollable(true)
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

	focusedView := func() (*tview.TextView, string) {
		if activeKey == "main" {
			return transcript, "main"
		}
		tv, _, _ := ensureSubPanel(activeKey)
		return tv, activeKey
	}

	clampOffset := func(text string, off int) int {
		if off < 0 {
			return 0
		}
		max := len(strings.Split(text, "\n")) - 1
		if max < 0 {
			max = 0
		}
		if off > max {
			return max
		}
		return off
	}

	scrollFocused := func(delta int) {
		tv, key := focusedView()
		text := tv.GetText(false)
		next := clampOffset(text, scrollOffset[key]+delta)
		scrollOffset[key] = next
		scrollLocked[key] = true
		tv.ScrollTo(next, 0)
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
			tv.SetText(renderTranscriptBody(panel.history, panel.streamingThinking, panel.streamingContent))
			if scrollLocked[sid] {
				scrollOffset[sid] = clampOffset(tv.GetText(false), scrollOffset[sid])
				tv.ScrollTo(scrollOffset[sid], 0)
			} else {
				tv.ScrollToEnd()
				scrollOffset[sid] = 1 << 30
			}
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
			delete(scrollOffset, sid)
			delete(scrollLocked, sid)
		}
		applyFocusStyle()
	}

	refresh := func() {
		body := renderTranscriptBody(m.history, m.streamingThinking, m.streamingContent)
		transcript.SetText(body)
		if scrollLocked["main"] {
			scrollOffset["main"] = clampOffset(transcript.GetText(false), scrollOffset["main"])
			transcript.ScrollTo(scrollOffset["main"], 0)
		} else {
			transcript.ScrollToEnd()
			scrollOffset["main"] = 1 << 30
		}
		status.SetText(m.statusLine())
		tokens.SetText(m.tokenSummaryLine())
		refreshSubPanels()
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
					if ev.ToolText != "" {
						m.history = append(m.history, strings.Split(ev.ToolText, "\n")...)
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
			unsub := ag.Subscribe(func(e communi.AgentEvent) {
				m.handleAgentEvent(e, &done, ch, subID)
			})
			defer unsub()
			err := ag.Prompt(context.Background(), prompt)
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
		text = strings.TrimSpace(text)
		if strings.HasPrefix(text, "/") {
			help.SetText(commandPaletteFromText(m, text))
		} else {
			help.SetText("Click panel to focus | Enter: send | Ctrl+C/Esc: abort/quit")
		}
	})

	panelContainer.AddItem(mainPanel, 0, 3, true)
	root = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(status, 1, 0, false).
		AddItem(panelContainer, 0, 1, true).
		AddItem(tokens, 1, 0, false).
		AddItem(help, 3, 0, false)

	app.SetRoot(root, true)
	app.SetFocus(mainInput)
	applyFocusStyle()
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		mu.Lock()
		defer mu.Unlock()

		switch event.Key() {
		case tcell.KeyCtrlC, tcell.KeyEscape:
			st := m.agent.State()
			if st.IsStreaming || mainStreaming {
				m.agent.Abort()
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
		switch action {
		case tview.MouseScrollUp:
			scrollFocused(-3)
			return nil, action
		case tview.MouseScrollDown:
			scrollFocused(3)
			return nil, action
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
			tv.SetText(renderTranscriptBody(panel.history, panel.streamingThinking, panel.streamingContent))
			if scrollLocked[sid] {
				scrollOffset[sid] = clampOffset(tv.GetText(false), scrollOffset[sid])
				tv.ScrollTo(scrollOffset[sid], 0)
			} else {
				tv.ScrollToEnd()
				scrollOffset[sid] = 1 << 30
			}
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
			delete(scrollOffset, sid)
			delete(scrollLocked, sid)
		}
		applyFocusStyle()
	}
	refresh()

	return app.Run()
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
