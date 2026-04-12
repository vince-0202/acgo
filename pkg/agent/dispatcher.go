package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/vince-0202/acgo/pkg/communi"
)

type DispatchRequest struct {
	Task            string
	Intent          string
	Constraints     []string
	PreferredSubIDs []string
}

type DispatchDecision struct {
	Target       string   `json:"target"`
	Reason       string   `json:"reason"`
	Alternatives []string `json:"alternatives,omitempty"`
	Confidence   float64  `json:"confidence,omitempty"`
}

type dispatchCandidate struct {
	subID string
	spec  SubAgentSpec
}

func (m *SubAgentManager) DispatchTask(ctx context.Context, req DispatchRequest) (DispatchDecision, error) {
	task := strings.TrimSpace(req.Task)
	intent := strings.TrimSpace(strings.ToLower(req.Intent))
	if task == "" {
		return DispatchDecision{}, fmt.Errorf("task is empty")
	}

	m.mu.Lock()
	all := make([]dispatchCandidate, 0, len(m.children))
	for subID := range m.children {
		all = append(all, dispatchCandidate{
			subID: subID,
			spec:  m.specs[subID],
		})
	}
	m.mu.Unlock()
	if len(all) == 0 {
		return DispatchDecision{}, fmt.Errorf("no sub-agents available")
	}

	preferred := make(map[string]bool)
	for _, item := range req.PreferredSubIDs {
		if key := normalizeSubAgentID(item); key != "" {
			preferred[key] = true
		}
	}

	filtered := make([]dispatchCandidate, 0, len(all))
	for _, candidate := range all {
		if len(preferred) > 0 && !preferred[candidate.subID] {
			continue
		}
		filtered = append(filtered, candidate)
	}
	if len(filtered) == 0 {
		filtered = all
	}

	ruleMatched := make([]dispatchCandidate, 0, len(filtered))
	for _, candidate := range filtered {
		if intent == "" {
			ruleMatched = append(ruleMatched, candidate)
			continue
		}
		if capabilityMatch(intent, candidate.spec.Capabilities) {
			ruleMatched = append(ruleMatched, candidate)
			continue
		}
		if keywordMatch(task, candidate.spec.Capabilities) {
			ruleMatched = append(ruleMatched, candidate)
		}
	}
	if len(ruleMatched) == 0 {
		ruleMatched = filtered
	}
	if len(ruleMatched) == 1 {
		return DispatchDecision{
			Target:     ruleMatched[0].subID,
			Reason:     "single candidate after rule filtering",
			Confidence: 0.9,
		}, nil
	}

	decision, err := m.llmDispatchChoice(ctx, task, intent, req.Constraints, ruleMatched)
	if err == nil && decision.Target != "" {
		return decision, nil
	}

	sort.SliceStable(ruleMatched, func(i, j int) bool {
		left := capabilityOverlapScore(task, intent, ruleMatched[i].spec.Capabilities)
		right := capabilityOverlapScore(task, intent, ruleMatched[j].spec.Capabilities)
		if left == right {
			return ruleMatched[i].subID < ruleMatched[j].subID
		}
		return left > right
	})
	alts := make([]string, 0, len(ruleMatched)-1)
	for i := 1; i < len(ruleMatched); i++ {
		alts = append(alts, ruleMatched[i].subID)
	}
	return DispatchDecision{
		Target:       ruleMatched[0].subID,
		Reason:       "fallback deterministic scorer",
		Alternatives: alts,
		Confidence:   0.6,
	}, nil
}

func capabilityMatch(intent string, caps []string) bool {
	intent = strings.TrimSpace(strings.ToLower(intent))
	for _, item := range caps {
		if strings.ToLower(strings.TrimSpace(item)) == intent {
			return true
		}
	}
	return false
}

func keywordMatch(task string, caps []string) bool {
	task = strings.ToLower(strings.TrimSpace(task))
	for _, item := range caps {
		if item == "" {
			continue
		}
		if strings.Contains(task, strings.ToLower(strings.TrimSpace(item))) {
			return true
		}
	}
	return false
}

func capabilityOverlapScore(task, intent string, caps []string) int {
	score := 0
	if capabilityMatch(intent, caps) {
		score += 3
	}
	for _, item := range caps {
		item = strings.TrimSpace(strings.ToLower(item))
		if item == "" {
			continue
		}
		if strings.Contains(strings.ToLower(task), item) {
			score++
		}
	}
	return score
}

func (m *SubAgentManager) llmDispatchChoice(ctx context.Context, task, intent string, constraints []string, candidates []dispatchCandidate) (DispatchDecision, error) {
	if m == nil || m.parent == nil || m.parent.provider == nil {
		return DispatchDecision{}, fmt.Errorf("provider unavailable")
	}

	type candidateProfile struct {
		SubID        string   `json:"sub_id"`
		Role         string   `json:"role,omitempty"`
		Capabilities []string `json:"capabilities,omitempty"`
		Constraints  []string `json:"constraints,omitempty"`
	}
	payload := struct {
		Task        string             `json:"task"`
		Intent      string             `json:"intent,omitempty"`
		Constraints []string           `json:"constraints,omitempty"`
		Candidates  []candidateProfile `json:"candidates"`
	}{
		Task:        task,
		Intent:      intent,
		Constraints: constraints,
		Candidates:  make([]candidateProfile, 0, len(candidates)),
	}
	for _, item := range candidates {
		payload.Candidates = append(payload.Candidates, candidateProfile{
			SubID:        item.subID,
			Role:         strings.TrimSpace(item.spec.Role),
			Capabilities: append([]string(nil), item.spec.Capabilities...),
			Constraints:  append([]string(nil), item.spec.Constraints...),
		})
	}
	body, _ := json.Marshal(payload)
	msgs := []communi.Message{
		communi.NewSystemMessageWithoutId("Select exactly one target sub-agent. Return JSON only: {\"target\":\"sub_id\",\"reason\":\"...\",\"confidence\":0.0}."),
		communi.NewUserMessageWithoutId(string(body)),
	}
	res, _, err := m.parent.provider.Complete(ctx, m.parent.model, msgs, nil)
	if err != nil {
		return DispatchDecision{}, err
	}
	text := strings.TrimSpace(res.ContentBlocksToText())
	if text == "" {
		return DispatchDecision{}, fmt.Errorf("empty llm dispatch output")
	}
	var parsed struct {
		Target     string  `json:"target"`
		Reason     string  `json:"reason"`
		Confidence float64 `json:"confidence"`
	}
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return DispatchDecision{}, err
	}
	target := normalizeSubAgentID(parsed.Target)
	if target == "" {
		return DispatchDecision{}, fmt.Errorf("invalid target")
	}
	alts := make([]string, 0, len(candidates)-1)
	for _, item := range candidates {
		if item.subID == target {
			continue
		}
		alts = append(alts, item.subID)
	}
	return DispatchDecision{
		Target:       target,
		Reason:       strings.TrimSpace(parsed.Reason),
		Alternatives: alts,
		Confidence:   parsed.Confidence,
	}, nil
}
