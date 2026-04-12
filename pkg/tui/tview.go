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
	"github.com/vince-0202/acgo/pkg/agent"
	"github.com/vince-0202/acgo/pkg/errors"
	"github.com/vince-0202/acgo/pkg/harness"
	"github.com/vince-0202/acgo/pkg/keys"
)

func tviewStyleLine(line string) string {
	switch {
	case strings.HasPrefix(line, "[Thinking]"):
		content := strings.TrimSpace(strings.TrimPrefix(line, "[Thinking]"))
		return "[#a9a9a9]" + tview.Escape("Thinking > "+content) + "[-]"
	default:
		return tview.Escape(line)
	}
}

func tviewTranscriptBody(history []string, streamThink, streamContent string) string {
	var b strings.Builder
	for _, line := range history {
		b.WriteString(tviewStyleLine(stripANSI(line)))
		b.WriteString("\n")
	}
	if streamThink != "" {
		b.WriteString("[#a9a9a9]" + tview.Escape("Thinking > "+stripANSI(streamThink)+"▌") + "[-]\n")
	}
	if streamContent != "" {
		b.WriteString(tview.Escape("Assistant: " + stripANSI(streamContent) + "▌"))
	}
	out := b.String()
	if strings.TrimSpace(out) == "" {
		return " "
	}
	return out
}

func tokenSummaryLineFromUsage(in, out, total int) string {
	return fmt.Sprintf("in: %d · out: %d · total: %d", in, out, total)
}

func rightStatusPanel(statusLine, tokenLine string, permissionMode string) string {
	var b strings.Builder
	b.WriteString("Tokens\n")
	b.WriteString("  " + strings.TrimSpace(tokenLine) + "\n\n")
	b.WriteString("Permission\n")
	if strings.TrimSpace(permissionMode) == "" {
		permissionMode = string(harness.PermissionModeDefault)
	}
	b.WriteString("  " + strings.TrimSpace(permissionMode) + "\n\n")
	b.WriteString("Status\n")
	b.WriteString("  " + strings.TrimSpace(statusLine))
	return b.String()
}

func leftPanelText(m *Model, input string, activePane string, selectedSubID string) string {
	input = strings.TrimSpace(input)
	if strings.HasPrefix(input, "/") {
		if p := strings.TrimSpace(m.commandPaletteFromText(input)); p != "" {
			return p
		}
	}
	subLine := selectedSubID
	if strings.TrimSpace(subLine) == "" {
		subLine = "(none)"
	}
	return strings.Join([]string{
		"ACGO (tview)",
		"",
		"Shortcuts:",
		"  Enter      send in active pane",
		"  Esc        abort/quit",
		"  Tab        switch pane (main/sub)",
		"  Shift+Tab  cycle permission mode",
		"  Ctrl+N/P   switch target sub",
		"  PgUp/PgDn  scroll active pane",
		"  Home/End   top/bottom",
		"",
		"Active pane: " + activePane,
		"Sub target: " + subLine,
		"",
		"Commands:",
		"  /help",
		"  /status",
		"  /abort",
		"  /session new",
		"  /compact",
	}, "\n")
}

func permissionPanelText(prompt string, selected int, options []string) string {
	var b strings.Builder
	b.WriteString("Permission request\n")
	b.WriteString("Use Up/Down + Enter (Esc to deny)\n\n")
	for i, opt := range options {
		prefix := "  "
		if i == selected {
			prefix = "> "
		}
		b.WriteString(prefix + opt + "\n")
	}
	if strings.TrimSpace(prompt) != "" {
		b.WriteString("\n")
		b.WriteString(strings.TrimSpace(prompt))
	}
	return b.String()
}

func runWithTView(m *Model) error {
	app := tview.NewApplication()
	app.EnableMouse(true)

	tview.Styles.PrimitiveBackgroundColor = tcell.GetColor("#f2f2f2")
	tview.Styles.ContrastBackgroundColor = tcell.GetColor("#e6e6e6")
	tview.Styles.MoreContrastBackgroundColor = tcell.GetColor("#dcdcdc")
	tview.Styles.BorderColor = tcell.GetColor("#666666")
	tview.Styles.TitleColor = tcell.GetColor("#222222")
	tview.Styles.PrimaryTextColor = tcell.GetColor("#222222")
	tview.Styles.SecondaryTextColor = tcell.GetColor("#444444")
	tview.Styles.TertiaryTextColor = tcell.GetColor("#666666")

	var mu sync.Mutex
	isStreamingMain := false
	subStreaming := map[string]bool{}
	activePane := "main" // "main" | "sub:<id>"
	selectedSubID := ""
	permissionMode := string(harness.PermissionModeDefault)

	permissionAwaiting := false
	permissionPrompt := ""
	permissionOptions := []string{"Allow", "Allow Always (session)", "Deny"}
	permissionSelected := 0
	permissionAllowSession := map[string]bool{}
	var permissionDecisionCh chan string
	var permissionPromptMu sync.Mutex
	var permissionController *harness.PermissionController
	pendingScheduledDispatch := false

	mainTranscript := tview.NewTextView().SetDynamicColors(true).SetWrap(true).SetScrollable(true)
	mainTranscript.SetBorder(true).SetTitle(" Main agent")
	leftPanel := tview.NewTextView().SetDynamicColors(true).SetWrap(true)
	leftPanel.SetBorder(true).SetTitle(" Commands")
	rightPanel := tview.NewTextView().SetDynamicColors(true).SetWrap(true)
	rightPanel.SetBorder(true).SetTitle(" Runtime")

	mainInput := tview.NewInputField().SetLabel("> ")
	mainInput.SetBorder(true).SetTitle(" Main input")

	subContainer := tview.NewFlex().SetDirection(tview.FlexColumn)
	subPanels := map[string]*tview.Flex{}
	subViews := map[string]*tview.TextView{}
	subInputs := map[string]*tview.InputField{}
	subScrollLocked := map[string]bool{}
	lastSubLayoutSig := ""

	getSubInfos := func() []agent.SubAgentInfo {
		infos := m.agent.SubAgentManager().List()
		sort.Slice(infos, func(i, j int) bool { return infos[i].ID < infos[j].ID })
		return infos
	}
	getSubIDs := func() []string {
		infos := getSubInfos()
		ids := make([]string, 0, len(infos))
		for _, info := range infos {
			ids = append(ids, info.ID)
		}
		return ids
	}
	normalizeSelectedSubID := func() {
		ids := getSubIDs()
		if len(ids) == 0 {
			selectedSubID = ""
			return
		}
		for _, id := range ids {
			if id == selectedSubID {
				return
			}
		}
		selectedSubID = ids[0]
	}
	cycleSubTarget := func(step int) {
		ids := getSubIDs()
		if len(ids) == 0 {
			selectedSubID = ""
			return
		}
		normalizeSelectedSubID()
		index := 0
		for i := range ids {
			if ids[i] == selectedSubID {
				index = i
				break
			}
		}
		index = (index + step + len(ids)) % len(ids)
		selectedSubID = ids[index]
	}
	subIDFromActivePane := func() (string, bool) {
		if !strings.HasPrefix(activePane, "sub:") {
			return "", false
		}
		id := strings.TrimSpace(strings.TrimPrefix(activePane, "sub:"))
		if id == "" {
			return "", false
		}
		return id, true
	}

	setActivePane := func(pane string) {
		pane = strings.TrimSpace(pane)
		if pane == "main" {
			activePane = "main"
			app.SetFocus(mainInput)
			return
		}
		if strings.HasPrefix(pane, "sub:") {
			id := strings.TrimSpace(strings.TrimPrefix(pane, "sub:"))
			if id != "" {
				if in, ok := subInputs[id]; ok && in != nil {
					activePane = "sub:" + id
					selectedSubID = id
					app.SetFocus(in)
					return
				}
			}
		}
		activePane = "main"
		app.SetFocus(mainInput)
	}

	scrollView := func(tv *tview.TextView, delta int) {
		if tv == nil {
			return
		}
		row, col := tv.GetScrollOffset()
		next := row + delta
		if next < 0 {
			next = 0
		}
		tv.ScrollTo(next, col)
	}
	scrollActive := func(delta int) {
		if sid, ok := subIDFromActivePane(); ok {
			scrollView(subViews[sid], delta)
			subScrollLocked[sid] = true
			return
		}
		scrollView(mainTranscript, delta)
		m.mainScrollLocked = true
	}
	scrollActiveTop := func() {
		if sid, ok := subIDFromActivePane(); ok {
			if tv := subViews[sid]; tv != nil {
				tv.ScrollToBeginning()
				subScrollLocked[sid] = true
			}
			return
		}
		mainTranscript.ScrollToBeginning()
		m.mainScrollLocked = true
	}
	scrollActiveBottom := func() {
		if sid, ok := subIDFromActivePane(); ok {
			if tv := subViews[sid]; tv != nil {
				tv.ScrollToEnd()
				subScrollLocked[sid] = false
			}
			return
		}
		mainTranscript.ScrollToEnd()
		m.mainScrollLocked = false
	}
	var refresh func()

	sendMain := func(raw string) (quit bool) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return false
		}
		mainInput.SetText("")
		if strings.HasPrefix(raw, "/") {
			reply, q := m.runCommand(raw)
			if strings.TrimSpace(reply) != "" {
				m.history = append(m.history, "> "+reply)
			}
			return q
		}
		if isStreamingMain {
			m.history = append(m.history, "> still streaming; use /abort")
			return false
		}
		m.history = append(m.history, "You: "+raw)
		isStreamingMain = true
		m.mainScrollLocked = false
		m.startAgentStream(raw, func(ev streamEvent) {
			app.QueueUpdateDraw(func() {
				mu.Lock()
				defer mu.Unlock()
				if ev.TurnUsage != nil {
					m.accumulateSessionUsage(ev.TurnUsage)
				}
				if ev.SubID != "" {
					m.applyStreamEventToSub(ev)
					if ev.Delta != "" || ev.ThinkingDelta != "" || ev.FinalContent != "" || ev.FinalThinking != "" || ev.ToolLine != nil {
						subScrollLocked[ev.SubID] = false
					}
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
					m.applyToolLine(ev.ToolLine)
				}
				if ev.ThinkingDelta != "" {
					m.streamingThinking += ev.ThinkingDelta
				}
				if ev.Delta != "" {
					m.streamingContent += ev.Delta
				}
				if ev.Delta != "" || ev.ThinkingDelta != "" || ev.FinalContent != "" || ev.FinalThinking != "" || ev.ToolLine != nil {
					m.mainScrollLocked = false
				}
				if ev.Done || ev.Err != nil {
					if ev.Err != nil {
						m.err = ev.Err
						if text := strings.TrimSpace(errors.FormatErrorForDisplay(ev.Err)); text != "" {
							m.history = append(m.history, "Error: "+text)
						}
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
					isStreamingMain = false
				}
				refresh()
			})
		})
		return false
	}

	startDirectSubStream := func(subID, prompt string) {
		subID = strings.TrimSpace(subID)
		if subID == "" {
			return
		}
		ch := make(chan streamEvent, 64)
		var done atomic.Bool
		go func() {
			child, ok := m.agent.SubAgentManager().Get(subID)
			if !ok || child == nil {
				ch <- streamEvent{Done: true, Err: fmt.Errorf("unknown sub-agent: %s", subID), Ch: ch, SubID: subID}
				close(ch)
				return
			}
			unsub := child.Subscribe(func(event agent.Event, abort func()) {
				m.handleAgentEvent(event, &done, ch, subID)
			})
			defer unsub()
			err := child.Prompt(context.Background(), prompt)
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
					if ev.Delta != "" || ev.ThinkingDelta != "" || ev.FinalContent != "" || ev.FinalThinking != "" || ev.ToolLine != nil {
						subScrollLocked[subID] = false
					}
					if ev.Done || ev.Err != nil {
						subStreaming[subID] = false
						if ev.Err != nil {
							p := m.ensureSubPanel(subID)
							if text := strings.TrimSpace(errors.FormatErrorForDisplay(ev.Err)); text != "" {
								p.history = append(p.history, "Error: "+text)
							}
						}
					}
					refresh()
				})
			}
		}()
	}

	sendSubByID := func(subID, raw string) {
		subID = strings.TrimSpace(subID)
		raw = strings.TrimSpace(raw)
		if subID == "" || raw == "" {
			return
		}
		if in := subInputs[subID]; in != nil {
			in.SetText("")
		}
		if subStreaming[subID] {
			p := m.ensureSubPanel(subID)
			p.history = append(p.history, "Error: sub-agent is busy")
			return
		}
		p := m.ensureSubPanel(subID)
		p.history = append(p.history, "You: "+raw)
		subStreaming[subID] = true
		subScrollLocked[subID] = false
		startDirectSubStream(subID, raw)
	}

	mainPanel := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(mainTranscript, 0, 6, false).
		AddItem(mainInput, 3, 0, true)
	panelContainer := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(mainPanel, 0, 1, true)
	bottomPanel := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(leftPanel, 0, 1, false).
		AddItem(rightPanel, 42, 0, false)
	root := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(panelContainer, 0, 1, true).
		AddItem(bottomPanel, 12, 0, false)

	rebuildSubPanels := func() {
		infos := getSubInfos()
		ids := make([]string, 0, len(infos))
		for _, info := range infos {
			ids = append(ids, info.ID)
		}
		sig := strings.Join(ids, ",")
		if sig != lastSubLayoutSig {
			subContainer.Clear()
			for _, info := range infos {
				sid := info.ID
				tv := subViews[sid]
				in := subInputs[sid]
				p := subPanels[sid]
				if tv == nil || in == nil || p == nil {
					tv = tview.NewTextView().SetDynamicColors(true).SetWrap(true).SetScrollable(true)
					in = tview.NewInputField().SetLabel("> ")
					p = tview.NewFlex().SetDirection(tview.FlexRow).
						AddItem(tv, 0, 6, false).
						AddItem(in, 3, 0, false)
					subViews[sid] = tv
					subInputs[sid] = in
					subPanels[sid] = p
					idCopy := sid
					in.SetDoneFunc(func(key tcell.Key) {
						if key != tcell.KeyEnter {
							return
						}
						mu.Lock()
						sendSubByID(idCopy, in.GetText())
						activePane = "sub:" + idCopy
						selectedSubID = idCopy
						refresh()
						mu.Unlock()
					})
					in.SetChangedFunc(func(text string) {
						if activePane == "sub:"+idCopy && !permissionAwaiting {
							leftPanel.SetText(leftPanelText(m, text, activePane, selectedSubID))
						}
					})
				}
				subContainer.AddItem(p, 0, 1, false)
			}

			for sid := range subViews {
				keep := false
				for _, id := range ids {
					if sid == id {
						keep = true
						break
					}
				}
				if !keep {
					delete(subViews, sid)
					delete(subInputs, sid)
					delete(subPanels, sid)
					delete(subStreaming, sid)
					delete(subScrollLocked, sid)
				}
			}
			lastSubLayoutSig = sig

			panelContainer.Clear()
			panelContainer.AddItem(mainPanel, 0, 6, true)
			if len(ids) > 0 {
				panelContainer.AddItem(subContainer, 0, 4, false)
			}
		}

		if len(ids) == 0 && strings.HasPrefix(activePane, "sub:") {
			activePane = "main"
			app.SetFocus(mainInput)
		}
		normalizeSelectedSubID()

		for _, info := range infos {
			sid := info.ID
			tv := subViews[sid]
			if tv == nil {
				continue
			}
			p := m.ensureSubPanel(sid)
			row, col := tv.GetScrollOffset()
			tv.SetText(tviewTranscriptBody(p.history, p.streamingThinking, p.streamingContent))
			if subScrollLocked[sid] {
				tv.ScrollTo(row, col)
			} else {
				tv.ScrollToEnd()
			}

			title := "Sub: " + sid
			if strings.TrimSpace(info.Role) != "" {
				title += " [" + strings.TrimSpace(info.Role) + "]"
			}
			if activePane == "sub:"+sid {
				title = "[::b]" + title + "[::-]"
				tv.SetBorderColor(tcell.ColorYellow)
				if in := subInputs[sid]; in != nil {
					in.SetBorderColor(tcell.ColorYellow)
				}
			} else {
				tv.SetBorderColor(tcell.ColorWhite)
				if in := subInputs[sid]; in != nil {
					in.SetBorderColor(tcell.ColorWhite)
				}
			}
			tv.SetBorder(true)
			tv.SetTitle(" " + title)
			if in := subInputs[sid]; in != nil {
				in.SetBorder(true)
				in.SetTitle(" Input")
			}
		}
	}

	refresh = func() {
		mainRow, mainCol := mainTranscript.GetScrollOffset()
		mainTranscript.SetText(tviewTranscriptBody(m.history, m.streamingThinking, m.streamingContent))
		if m.mainScrollLocked {
			mainTranscript.ScrollTo(mainRow, mainCol)
		} else {
			mainTranscript.ScrollToEnd()
		}
		if activePane == "main" {
			mainTranscript.SetTitle(" [::b]Main agent[::-]")
			mainInput.SetBorderColor(tcell.ColorYellow)
		} else {
			mainTranscript.SetTitle(" Main agent")
			mainInput.SetBorderColor(tcell.ColorWhite)
		}

		rebuildSubPanels()

		rightPanel.SetText(rightStatusPanel(
			m.statusLine(),
			tokenSummaryLineFromUsage(m.usageIn, m.usageOut, m.usageTotal),
			permissionMode,
		))
		if permissionAwaiting {
			leftPanel.SetText(permissionPanelText(permissionPrompt, permissionSelected, permissionOptions))
			return
		}
		activeInput := mainInput.GetText()
		if sid, ok := subIDFromActivePane(); ok {
			if in := subInputs[sid]; in != nil {
				activeInput = in.GetText()
			}
		}
		leftPanel.SetText(leftPanelText(m, activeInput, activePane, selectedSubID))
	}

	mainInput.SetDoneFunc(func(key tcell.Key) {
		if key != tcell.KeyEnter {
			return
		}
		mu.Lock()
		quit := sendMain(mainInput.GetText())
		refresh()
		mu.Unlock()
		if quit {
			app.Stop()
		}
	})
	mainInput.SetChangedFunc(func(text string) {
		if activePane == "main" && !permissionAwaiting {
			leftPanel.SetText(leftPanelText(m, text, activePane, selectedSubID))
		}
	})

	confirmHook := func(ctx context.Context, req harness.PermissionRequest) (bool, string, error) {
		permissionPromptMu.Lock()
		defer permissionPromptMu.Unlock()

		key := strings.TrimSpace(req.Action) + "|" + strings.TrimSpace(req.Resource)
		mu.Lock()
		allowed := permissionAllowSession[key]
		mu.Unlock()
		if allowed {
			return true, "approved by tui user (session remember)", nil
		}

		toolName, _ := req.Metadata["tool_name"].(string)
		toolArgs, _ := req.Metadata["tool_args"].(string)
		if strings.TrimSpace(toolName) == "" {
			toolName = req.Resource
		}
		var prompt strings.Builder
		prompt.WriteString("Tool: " + strings.TrimSpace(toolName) + "\n")
		prompt.WriteString("Action: " + strings.TrimSpace(req.Action) + "\n")
		prompt.WriteString("Resource: " + strings.TrimSpace(req.Resource) + "\n")
		if strings.TrimSpace(toolArgs) != "" {
			args := strings.TrimSpace(toolArgs)
			if len(args) > 800 {
				args = args[:800] + "..."
			}
			prompt.WriteString("\nArgs:\n" + args + "\n")
		}

		decisionCh := make(chan string, 1)
		mu.Lock()
		permissionAwaiting = true
		permissionPrompt = strings.TrimSpace(prompt.String())
		permissionSelected = 0
		permissionDecisionCh = decisionCh
		mu.Unlock()
		app.QueueUpdateDraw(func() {
			mu.Lock()
			refresh()
			mu.Unlock()
		})
		defer func() {
			app.QueueUpdateDraw(func() {
				mu.Lock()
				permissionAwaiting = false
				permissionPrompt = ""
				permissionSelected = 0
				permissionDecisionCh = nil
				refresh()
				mu.Unlock()
			})
		}()

		select {
		case decision := <-decisionCh:
			switch decision {
			case "allow_always":
				mu.Lock()
				permissionAllowSession[key] = true
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
	}

	if m.harness != nil {
		for _, controller := range m.harness.Controllers {
			if pc, ok := controller.(*harness.PermissionController); ok && pc != nil {
				permissionController = pc
				pc.SetConfirmHook(confirmHook)
				permissionMode = string(pc.Mode())
			}
		}
	}

	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		mu.Lock()
		defer mu.Unlock()

		if permissionAwaiting {
			switch event.Key() {
			case tcell.KeyUp:
				if permissionSelected > 0 {
					permissionSelected--
				}
				refresh()
				return nil
			case tcell.KeyDown:
				if permissionSelected < len(permissionOptions)-1 {
					permissionSelected++
				}
				refresh()
				return nil
			case tcell.KeyEnter:
				if permissionDecisionCh != nil {
					decision := "deny"
					switch permissionSelected {
					case 0:
						decision = "allow_once"
					case 1:
						decision = "allow_always"
					}
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
			if permissionController != nil {
				sequence := []harness.PermissionMode{
					harness.PermissionModeDefault,
					harness.PermissionModeAcceptEdits,
					harness.PermissionModePlan,
					harness.PermissionModeAuto,
					harness.PermissionModeBypass,
				}
				current := permissionController.Mode()
				idx := 0
				for i := range sequence {
					if sequence[i] == current {
						idx = i
						break
					}
				}
				next := sequence[(idx+1)%len(sequence)]
				if err := permissionController.SetMode(next); err != nil {
					m.history = append(m.history, "Error: set permission mode failed: "+err.Error())
				} else {
					permissionMode = string(next)
					m.history = append(m.history, "System: permission mode -> "+permissionMode)
				}
			}
			refresh()
			return nil
		case tcell.KeyTab:
			if sid, ok := subIDFromActivePane(); ok && sid != "" {
				setActivePane("main")
			} else {
				normalizeSelectedSubID()
				if selectedSubID != "" {
					setActivePane("sub:" + selectedSubID)
				}
			}
			refresh()
			return nil
		case tcell.KeyCtrlN:
			cycleSubTarget(+1)
			if _, ok := subIDFromActivePane(); ok && selectedSubID != "" {
				setActivePane("sub:" + selectedSubID)
			}
			refresh()
			return nil
		case tcell.KeyCtrlP:
			cycleSubTarget(-1)
			if _, ok := subIDFromActivePane(); ok && selectedSubID != "" {
				setActivePane("sub:" + selectedSubID)
			}
			refresh()
			return nil
		case tcell.KeyPgUp:
			scrollActive(-12)
			return nil
		case tcell.KeyPgDn:
			scrollActive(+12)
			return nil
		case tcell.KeyHome:
			scrollActiveTop()
			return nil
		case tcell.KeyEnd:
			scrollActiveBottom()
			return nil
		case tcell.KeyCtrlC, tcell.KeyEscape:
			if isStreamingMain {
				m.agent.Abort()
				return nil
			}
			for sid, running := range subStreaming {
				if !running {
					continue
				}
				if child, ok := m.agent.SubAgentManager().Get(sid); ok && child != nil {
					child.Abort()
				}
			}
			anySubRunning := false
			for _, running := range subStreaming {
				if running {
					anySubRunning = true
					break
				}
			}
			if anySubRunning {
				return nil
			}
			app.Stop()
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
			if inRect(mainTranscript) || inRect(mainInput) {
				setActivePane("main")
			} else {
				for sid := range subViews {
					if inRect(subViews[sid]) || inRect(subInputs[sid]) {
						setActivePane("sub:" + sid)
						selectedSubID = sid
						break
					}
				}
			}
			refresh()
		}

		switch action {
		case tview.MouseScrollUp:
			if inRect(mainTranscript) {
				setActivePane("main")
				scrollView(mainTranscript, -3)
				m.mainScrollLocked = true
				refresh()
				return nil, tview.MouseConsumed
			}
			for sid := range subViews {
				if inRect(subViews[sid]) {
					setActivePane("sub:" + sid)
					scrollView(subViews[sid], -3)
					subScrollLocked[sid] = true
					refresh()
					return nil, tview.MouseConsumed
				}
			}
			scrollActive(-3)
			refresh()
			return nil, tview.MouseConsumed
		case tview.MouseScrollDown:
			if inRect(mainTranscript) {
				setActivePane("main")
				scrollView(mainTranscript, 3)
				m.mainScrollLocked = true
				refresh()
				return nil, tview.MouseConsumed
			}
			for sid := range subViews {
				if inRect(subViews[sid]) {
					setActivePane("sub:" + sid)
					scrollView(subViews[sid], 3)
					subScrollLocked[sid] = true
					refresh()
					return nil, tview.MouseConsumed
				}
			}
			scrollActive(3)
			refresh()
			return nil, tview.MouseConsumed
		}
		return event, action
	})

	app.SetRoot(root, true)
	backgroundUnsub := m.agent.Subscribe(func(event agent.Event, abort func()) {
		if event.Message == nil {
			return
		}
		if event.Type != agent.EventMessageEnd && event.Type != agent.EventToolExecutionEnd {
			return
		}
		if m.streamDone != nil && !m.streamDone.Load() {
			// Active foreground stream already consumes and renders these events.
			return
		}
		app.QueueUpdateDraw(func() {
			mu.Lock()
			defer mu.Unlock()
			if m.session != nil {
				_ = m.session.AppendMessage(*event.Message)
			}
			switch event.Type {
			case agent.EventMessageEnd:
				if event.Message.Role == keys.AgentRoleUser && event.Message.SuppressTranscript() {
					// A hidden scheduled dispatch message just arrived; mark the next assistant message.
					pendingScheduledDispatch = true
					return
				}
				if event.Message.Role != keys.AgentRoleAssistant {
					return
				}
				finalThinking := strings.TrimSpace(event.Message.Thinking)
				finalContent := strings.TrimSpace(event.Message.ContentBlocksToText())
				if finalThinking == "" && finalContent == "" {
					pendingScheduledDispatch = false
					return
				}
				prefix := ""
				if pendingScheduledDispatch {
					prefix = "Reminder: "
					pendingScheduledDispatch = false
				}
				if finalThinking != "" {
					m.history = append(m.history, "[Thinking] "+finalThinking)
				}
				if finalContent != "" {
					m.history = append(m.history, "Assistant: "+prefix+finalContent)
				}
				m.mainScrollLocked = false
			case agent.EventToolExecutionEnd:
				if event.Tool == nil {
					return
				}
				toolName := strings.TrimSpace(event.Tool.Name())
				if toolName == "" {
					return
				}
				status := "ok"
				if event.Error != nil || event.Message.IsError {
					status = "error"
				}
				m.history = append(m.history, formatToolLine(toolName, status))
				m.mainScrollLocked = false
			}
			refresh()
		})
	})
	defer backgroundUnsub()

	mu.Lock()
	refresh()
	setActivePane("main")
	mu.Unlock()
	return app.Run()
}
