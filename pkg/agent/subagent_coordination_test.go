package agent

import (
	"context"
	"testing"

	"github.com/vince-0202/acgo/pkg/harness"
)

func newTestParentAgent() *Agent {
	return New("parent-test", Options{
		WorkDir: "/tmp",
	})
}

func TestCreateWithProfileAndListRole(t *testing.T) {
	parent := newTestParentAgent()
	sac := NewSubAgentController(parent)
	_, err := sac.CreateWithOptions(harness.SubAgentCreateOptions{
		SubID: "dev",
		Profile: harness.SubAgentProfile{
			Role:         "developer",
			RolePrompt:   "write implementation code",
			Capabilities: []string{"implement", "refactor"},
		},
	})
	if err != nil {
		t.Fatalf("CreateWithOptions failed: %v", err)
	}

	rows := sac.List()
	if len(rows) != 1 {
		t.Fatalf("List len = %d, want 1", len(rows))
	}
	if rows[0].Role != "developer" {
		t.Fatalf("List role = %q, want %q", rows[0].Role, "developer")
	}
	prof, ok := sac.GetProfile("dev")
	if !ok {
		t.Fatalf("GetProfile missing")
	}
	if prof.RolePrompt == "" {
		t.Fatalf("RolePrompt should be set")
	}
}

func TestMessageBusSendPullAck(t *testing.T) {
	parent := newTestParentAgent()
	sac := NewSubAgentController(parent)
	_, _ = sac.CreateWithOptions(harness.SubAgentCreateOptions{
		SubID:   "planner",
		Profile: harness.SubAgentProfile{Capabilities: []string{"plan"}, AllowedPeers: []string{"coder"}},
	})
	_, _ = sac.CreateWithOptions(harness.SubAgentCreateOptions{
		SubID:   "coder",
		Profile: harness.SubAgentProfile{Capabilities: []string{"implement"}},
	})

	msg, err := sac.SendMessage("planner", "coder", "implement", "do task", "corr-1", nil)
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}
	inbox := sac.PullInbox("coder", 10, "")
	if len(inbox) != 1 {
		t.Fatalf("PullInbox len = %d, want 1", len(inbox))
	}
	if inbox[0].ID != msg.ID {
		t.Fatalf("PullInbox got %q, want %q", inbox[0].ID, msg.ID)
	}
	acked, err := sac.AckMessage(msg.ID)
	if err != nil {
		t.Fatalf("AckMessage failed: %v", err)
	}
	if acked.AckedAt == nil {
		t.Fatalf("AckedAt should not be nil")
	}
}

func TestDispatchFallbackDeterministic(t *testing.T) {
	parent := newTestParentAgent()
	sac := NewSubAgentController(parent)
	_, _ = sac.CreateWithOptions(harness.SubAgentCreateOptions{
		SubID:   "search",
		Profile: harness.SubAgentProfile{Capabilities: []string{"search", "research"}},
	})
	_, _ = sac.CreateWithOptions(harness.SubAgentCreateOptions{
		SubID:   "coder",
		Profile: harness.SubAgentProfile{Capabilities: []string{"implement", "code"}},
	})

	decision, err := sac.DispatchTask(context.Background(), DispatchRequest{
		Task:   "please implement a cron task manager",
		Intent: "implement",
	})
	if err != nil {
		t.Fatalf("DispatchTask failed: %v", err)
	}
	if decision.Target != "coder" {
		t.Fatalf("Dispatch target = %q, want %q", decision.Target, "coder")
	}
}
