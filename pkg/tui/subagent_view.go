package tui

import (
	"strings"
)

func (m *Model) ensureSubPanel(subID string) *subAgentPanelState {
	subID = strings.TrimSpace(subID)
	if subID == "" {
		subID = "unknown"
	}
	if p := m.subAgentPanels[subID]; p != nil {
		return p
	}
	p := &subAgentPanelState{
		toolPendingIdx: make(map[string]int),
	}
	m.subAgentPanels[subID] = p
	return p
}

func (m *Model) pruneSubAgentPanels() {
	listed := make(map[string]bool)
	for _, info := range m.agent.SubAgentManager().List() {
		listed[info.ID] = true
	}
	for id := range m.subAgentPanels {
		if listed[id] {
			continue
		}
		delete(m.subAgentPanels, id)
	}
}
