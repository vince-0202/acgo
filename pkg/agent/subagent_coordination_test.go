package agent

import (
	"context"
	"errors"
	"testing"
)

func newTestAgent(id, workDir string) *Agent {
	return New(Options{
		ID:       id,
		WorkDir:  workDir,
		Provider: nil,
	})
}

func TestDispatchFallbackDeterministic(t *testing.T) {
	root := t.TempDir()
	parent := newTestAgent("parent-test", root)
	manager := parent.SubAgentManager().(*SubAgentManager)

	coder := newTestAgent("child-coder", root)
	searcher := newTestAgent("child-search", root)
	if err := manager.Register("search", SubAgentSpec{Capabilities: []string{"search", "research"}}, searcher); err != nil {
		t.Fatalf("register search: %v", err)
	}
	if err := manager.Register("coder", SubAgentSpec{Capabilities: []string{"implement", "code"}}, coder); err != nil {
		t.Fatalf("register coder: %v", err)
	}

	decision, err := manager.DispatchTask(context.Background(), DispatchRequest{
		Task:   "please implement a cron task manager",
		Intent: "implement",
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if decision.Target != "coder" {
		t.Fatalf("dispatch target = %q, want %q", decision.Target, "coder")
	}
}

func TestMessageBusSendPullAck(t *testing.T) {
	root := t.TempDir()
	parent := newTestAgent("parent-test", root)
	manager := parent.SubAgentManager().(*SubAgentManager)

	planner := newTestAgent("child-planner", root)
	coder := newTestAgent("child-coder", root)
	if err := manager.Register("planner", SubAgentSpec{
		Capabilities: []string{"plan"},
		AllowedPeers: []string{"coder"},
	}, planner); err != nil {
		t.Fatalf("register planner: %v", err)
	}
	if err := manager.Register("coder", SubAgentSpec{
		Capabilities: []string{"implement"},
	}, coder); err != nil {
		t.Fatalf("register coder: %v", err)
	}

	msg, err := manager.SendMessage("planner", "coder", "implement", "do task", "corr-1", nil)
	if err != nil {
		t.Fatalf("send message: %v", err)
	}
	inbox := manager.PullInbox("coder", 10, "")
	if len(inbox) != 1 {
		t.Fatalf("inbox length = %d, want 1", len(inbox))
	}
	if inbox[0].ID != msg.ID {
		t.Fatalf("inbox id = %q, want %q", inbox[0].ID, msg.ID)
	}
	acked, err := manager.AckMessage(msg.ID)
	if err != nil {
		t.Fatalf("ack message: %v", err)
	}
	if acked.AckedAt == nil {
		t.Fatalf("acked_at should not be nil")
	}
}

func TestMessageRouteDeniedReturnsStructuredError(t *testing.T) {
	root := t.TempDir()
	parent := newTestAgent("parent-test", root)
	manager := parent.SubAgentManager().(*SubAgentManager)

	planner := newTestAgent("child-planner", root)
	coder := newTestAgent("child-coder", root)
	reviewer := newTestAgent("child-reviewer", root)
	if err := manager.Register("planner", SubAgentSpec{
		Capabilities: []string{"plan"},
		AllowedPeers: []string{"coder"},
	}, planner); err != nil {
		t.Fatalf("register planner: %v", err)
	}
	if err := manager.Register("coder", SubAgentSpec{Capabilities: []string{"implement"}}, coder); err != nil {
		t.Fatalf("register coder: %v", err)
	}
	if err := manager.Register("reviewer", SubAgentSpec{Capabilities: []string{"review"}}, reviewer); err != nil {
		t.Fatalf("register reviewer: %v", err)
	}

	_, err := manager.SendMessage("planner", "reviewer", "review", "please review", "corr-2", nil)
	if err == nil {
		t.Fatalf("expected route denied error")
	}
	var msgErr *SubAgentMessageError
	if !errors.As(err, &msgErr) {
		t.Fatalf("expected SubAgentMessageError, got %T", err)
	}
	if msgErr.Code != "route_denied" {
		t.Fatalf("error code = %q, want %q", msgErr.Code, "route_denied")
	}
	if msgErr.FromSubID != "planner" || msgErr.ToSubID != "reviewer" {
		t.Fatalf("unexpected route fields: from=%q to=%q", msgErr.FromSubID, msgErr.ToSubID)
	}
}
