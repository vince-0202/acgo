package agent

import (
	"context"
	"testing"
)

func TestTransformContext_KeepsSystemAndTrimsByTurns(t *testing.T) {
	opts := TransformContextOptions{MaxTurns: 2, MinToolResultsToKeep: 0}
	msgs := []AgentMessage{
		{Role: RoleSystem, Content: "You are helpful."},
		{Role: RoleUser, Content: "turn1"},
		{Role: RoleAssistant, Content: "r1"},
		{Role: RoleUser, Content: "turn2"},
		{Role: RoleAssistant, Content: "r2"},
		{Role: RoleUser, Content: "turn3"},
		{Role: RoleAssistant, Content: "r3"},
	}
	out := transformContextWithOptions(msgs, opts)
	if len(out) != 5 {
		t.Fatalf("got %d messages, want 5 (system + last 2 turns = 4 msgs)", len(out))
	}
	if out[0].Role != RoleSystem || out[0].Content != "You are helpful." {
		t.Errorf("first message: role=%s content=%q", out[0].Role, out[0].Content)
	}
	if out[1].Content != "turn2" || out[3].Content != "turn3" {
		t.Errorf("expected turn2 and turn3; got %q, %q", out[1].Content, out[3].Content)
	}
}

func TestTransformContext_KeepsLastToolResults(t *testing.T) {
	opts := TransformContextOptions{MaxTurns: 1, MinToolResultsToKeep: 2}
	msgs := []AgentMessage{
		{Role: RoleSystem, Content: "Sys"},
		{Role: RoleUser, Content: "u1"},
		{Role: RoleAssistant, Content: "a1"},
		{Role: RoleTool, Content: "t1", ToolCallID: "c1"},
		{Role: RoleUser, Content: "u2"},
		{Role: RoleAssistant, Content: "a2"},
		{Role: RoleTool, Content: "t2", ToolCallID: "c2"},
		{Role: RoleTool, Content: "t3", ToolCallID: "c3"},
	}
	out := transformContextWithOptions(msgs, opts)
	// Should keep system + messages that include last 2 tool results (t2, t3). So we need u2, a2, t2, t3 at least.
	if len(out) < 5 {
		t.Fatalf("got %d messages, want at least 5 (system + u2, a2, t2, t3)", len(out))
	}
	toolContents := []string{}
	for _, m := range out {
		if m.Role == RoleTool {
			toolContents = append(toolContents, m.Content)
		}
	}
	if len(toolContents) < 2 || toolContents[len(toolContents)-2] != "t2" || toolContents[len(toolContents)-1] != "t3" {
		t.Errorf("expected last two tool results t2, t3; got %v", toolContents)
	}
}

func TestTransformContext_EmptyAndNoConv(t *testing.T) {
	opts := DefaultTransformContextOptions()
	if got := transformContextWithOptions(nil, opts); got != nil {
		t.Errorf("nil input: got %v", got)
	}
	systemOnly := []AgentMessage{{Role: RoleSystem, Content: "Only"}}
	out := transformContextWithOptions(systemOnly, opts)
	if len(out) != 1 || out[0].Content != "Only" {
		t.Errorf("system only: got %v", out)
	}
}

func TestDefaultTransformContext_PassthroughWhenShort(t *testing.T) {
	msgs := []AgentMessage{
		{Role: RoleUser, Content: "hi"},
		{Role: RoleAssistant, Content: "hello"},
	}
	out := defaultTransformContext(msgs, context.Background())
	if len(out) != 2 {
		t.Fatalf("short list: got %d messages", len(out))
	}
	if out[0].Content != "hi" || out[1].Content != "hello" {
		t.Errorf("content changed: %v", out)
	}
}

func TestEstimateMessageTokens(t *testing.T) {
	m := AgentMessage{Content: "hello world"} // 11 chars -> ~3 tokens
	if n := estimateMessageTokens(m); n < 2 || n > 4 {
		t.Errorf("estimateMessageTokens(hello world) = %d, want ~3", n)
	}
	m2 := AgentMessage{Content: "x"}
	if n := estimateMessageTokens(m2); n != 1 {
		t.Errorf("estimateMessageTokens(x) = %d, want 1", n)
	}
}
