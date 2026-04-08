package harness

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/vince-0202/acgo/pkg/communi"
)

func TestPermissionControllerModes(t *testing.T) {
	t.Run("acceptEdits allows file edit tools by default", func(t *testing.T) {
		pc := NewPermissionController(PermissionModeAcceptEdits, nil, nil)
		_, err := pc.Check(context.Background(), PermissionRequest{
			Action:   "tool.execute",
			Resource: "tool:write",
		})
		if err != nil {
			t.Fatalf("expected allow, got error: %v", err)
		}
	})

	t.Run("plan denies file edit tools by default", func(t *testing.T) {
		pc := NewPermissionController(PermissionModePlan, nil, nil)
		_, err := pc.Check(context.Background(), PermissionRequest{
			Action:   "tool.execute",
			Resource: "tool:write",
		})
		if err == nil {
			t.Fatalf("expected deny error, got nil")
		}
	})

	t.Run("default confirms non-read tools without hook", func(t *testing.T) {
		pc := NewPermissionController(PermissionModeDefault, nil, nil)
		_, err := pc.Check(context.Background(), PermissionRequest{
			Action:   "tool.execute",
			Resource: "tool:bash",
		})
		if err == nil {
			t.Fatalf("expected confirm error, got nil")
		}
	})

	t.Run("default allows read tools", func(t *testing.T) {
		pc := NewPermissionController(PermissionModeDefault, nil, nil)
		_, err := pc.Check(context.Background(), PermissionRequest{
			Action:   "tool.execute",
			Resource: "tool:read",
		})
		if err != nil {
			t.Fatalf("expected allow for read tool, got error: %v", err)
		}
	})

	t.Run("auto allows non-read tools by default", func(t *testing.T) {
		pc := NewPermissionController(PermissionModeAuto, nil, nil)
		_, err := pc.Check(context.Background(), PermissionRequest{
			Action:   "tool.execute",
			Resource: "tool:bash",
		})
		if err != nil {
			t.Fatalf("expected auto allow for bash tool, got error: %v", err)
		}
	})

	t.Run("bypass allows by default and ignores rules", func(t *testing.T) {
		pc := NewPermissionController(PermissionModeBypass, []PermissionRule{
			{Action: "tool.execute", Resource: "tool:write", Effect: PermissionEffectDeny},
		}, nil)
		_, err := pc.Check(context.Background(), PermissionRequest{
			Action:   "tool.execute",
			Resource: "tool:write",
		})
		if err != nil {
			t.Fatalf("expected bypass allow, got error: %v", err)
		}
	})

	t.Run("bypass confirms protected write", func(t *testing.T) {
		pc := NewPermissionController(PermissionModeBypass, nil, nil)
		_, err := pc.Check(context.Background(), PermissionRequest{
			Action:   "tool.execute",
			Resource: "tool:write",
			Metadata: map[string]any{
				"tool_name": "write",
				"tool_args": `{"path":".git/config","content":"x"}`,
			},
		})
		if err == nil {
			t.Fatalf("expected protected write confirm error, got nil")
		}
	})
}

func TestPermissionControllerConfirmHookAndRulePriority(t *testing.T) {
	pc := NewPermissionController(PermissionModeDefault, []PermissionRule{
		{Action: "tool.execute", Resource: "tool:shell", Effect: PermissionEffectDeny},
		{Action: "tool.execute", Resource: "tool:shell", Effect: PermissionEffectAllow},
	}, func(ctx context.Context, req PermissionRequest) (bool, string, error) {
		return true, "approved", nil
	})
	_, err := pc.Check(context.Background(), PermissionRequest{
		Action:   "tool.execute",
		Resource: "tool:shell",
	})
	if err == nil {
		t.Fatalf("expected first matching deny rule to win")
	}

	pc2 := NewPermissionController(PermissionModeDefault, []PermissionRule{
		{Action: "tool.execute", Resource: "tool:delete", Effect: PermissionEffectConfirm},
	}, func(ctx context.Context, req PermissionRequest) (bool, string, error) {
		return true, "approved by hook", nil
	})
	result, err := pc2.Check(context.Background(), PermissionRequest{
		Action:   "tool.execute",
		Resource: "tool:delete",
	})
	if err != nil {
		t.Fatalf("expected confirm hook allow, got error: %v", err)
	}
	if result.Decision != PermissionDecisionAllow {
		t.Fatalf("expected allow decision, got %q", result.Decision)
	}
}

func TestToolControllerPermissionGate(t *testing.T) {
	t.Run("denied request does not execute tool", func(t *testing.T) {
		tool := &countingTool{name: "shell"}
		tc := NewToolController(
			"agent-test",
			NewContextController("agent-test", "."),
			func(e communi.AgentEvent) {},
			NewPermissionController(PermissionModePlan, []PermissionRule{
				{Action: "tool.execute", Resource: "tool:read_only", Effect: PermissionEffectAllow},
			}, nil),
			tool,
		)
		tc.UpsertPendingToolCall(communi.ToolCallRequest{
			ID:        "tc1",
			Name:      "shell",
			Arguments: json.RawMessage(`{"cmd":"pwd"}`),
		})
		tc.Execute(context.Background())
		if tool.calls != 0 {
			t.Fatalf("expected tool not to be executed when permission denied")
		}
	})

	t.Run("allowed request executes tool", func(t *testing.T) {
		tool := &countingTool{name: "shell"}
		tc := NewToolController(
			"agent-test",
			NewContextController("agent-test", "."),
			func(e communi.AgentEvent) {},
			NewPermissionController(PermissionModePlan, []PermissionRule{
				{Action: "tool.execute", Resource: "tool:shell", Effect: PermissionEffectAllow},
			}, nil),
			tool,
		)
		tc.UpsertPendingToolCall(communi.ToolCallRequest{
			ID:        "tc2",
			Name:      "shell",
			Arguments: json.RawMessage(`{"cmd":"pwd"}`),
		})
		tc.Execute(context.Background())
		if tool.calls != 1 {
			t.Fatalf("expected tool to execute exactly once, got %d", tool.calls)
		}
	})
}

func TestPermissionControllerSetMode(t *testing.T) {
	pc := NewPermissionController(PermissionModeDefault, nil, nil)
	if got := pc.Mode(); got != PermissionModeDefault {
		t.Fatalf("expected default mode, got %q", got)
	}

	if err := pc.SetMode(PermissionModeAcceptEdits); err != nil {
		t.Fatalf("set mode acceptEdits: %v", err)
	}
	if got := pc.Mode(); got != PermissionModeAcceptEdits {
		t.Fatalf("expected acceptEdits mode, got %q", got)
	}

	if err := pc.SetMode(""); err != nil {
		t.Fatalf("set empty mode should normalize to default: %v", err)
	}
	if got := pc.Mode(); got != PermissionModeDefault {
		t.Fatalf("expected normalized default mode, got %q", got)
	}

	if err := pc.SetMode(PermissionModeAuto); err != nil {
		t.Fatalf("set mode auto: %v", err)
	}
	if got := pc.Mode(); got != PermissionModeAuto {
		t.Fatalf("expected auto mode, got %q", got)
	}

	if err := pc.SetMode(PermissionModeBypass); err != nil {
		t.Fatalf("set mode bypassPermissions: %v", err)
	}
	if got := pc.Mode(); got != PermissionModeBypass {
		t.Fatalf("expected bypassPermissions mode, got %q", got)
	}

	if err := pc.SetMode(PermissionMode("invalid")); err == nil {
		t.Fatalf("expected invalid mode error, got nil")
	}
}

type countingTool struct {
	name  string
	calls int
}

func (t *countingTool) Name() string { return t.name }
func (t *countingTool) Label() string {
	return t.name
}
func (t *countingTool) Description() string { return "counting tool" }
func (t *countingTool) JSONSchema() map[string]any {
	return map[string]any{
		"type": "object",
	}
}
func (t *countingTool) Execute(ctx context.Context, toolCallID string, args json.RawMessage, update ToolUpdateFunc) communi.ToolCallResult {
	t.calls++
	return communi.NewToolCallResult(toolCallID, "ok")
}
