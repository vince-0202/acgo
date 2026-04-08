package harness

import (
	"context"
	"testing"

	"github.com/vince-0202/acgo/pkg/communi"
	"github.com/vince-0202/acgo/pkg/keys"
	"github.com/vince-0202/acgo/pkg/llm"
)

func TestSplitLeadingSystemAndTurns(t *testing.T) {
	msgs := []communi.Message{
		{Role: keys.AgentRoleSystem, Content: []*communi.ContentBlock{communi.NewTextContentBlock("sys")}},
		{Role: keys.AgentRoleUser, Content: []*communi.ContentBlock{communi.NewTextContentBlock("u1")}},
		{Role: keys.AgentRoleAssistant, Content: []*communi.ContentBlock{communi.NewTextContentBlock("a1")}},
		{Role: keys.AgentRoleUser, Content: []*communi.ContentBlock{communi.NewTextContentBlock("u2")}},
	}
	prefix, body := splitLeadingSystem(msgs)
	if len(prefix) != 1 || len(body) != 3 {
		t.Fatalf("prefix/body len got %d %d", len(prefix), len(body))
	}
	turns := segmentTurnsByUser(body)
	if len(turns) != 2 {
		t.Fatalf("turns want 2 got %d", len(turns))
	}
}

func TestApplyDeterministicTrimMaxTurns(t *testing.T) {
	cc := NewContextController("a", ".")
	cc.Options = &ContextOptions{MaxTurns: 1}
	cc.Messages = []communi.Message{
		{Role: keys.AgentRoleSystem, Content: []*communi.ContentBlock{communi.NewTextContentBlock("s")}},
		{Role: keys.AgentRoleUser, Content: []*communi.ContentBlock{communi.NewTextContentBlock("u1")}},
		{Role: keys.AgentRoleAssistant, Content: []*communi.ContentBlock{communi.NewTextContentBlock("a1")}},
		{Role: keys.AgentRoleUser, Content: []*communi.ContentBlock{communi.NewTextContentBlock("u2")}},
		{Role: keys.AgentRoleAssistant, Content: []*communi.ContentBlock{communi.NewTextContentBlock("a2")}},
	}
	cc.applyDeterministicTrim(cc.Options)
	if len(cc.Messages) != 3 {
		t.Fatalf("expected system + last turn (3 msgs), got %d", len(cc.Messages))
	}
	if cc.Messages[1].ContentBlocksToText() != "u2" {
		t.Fatalf("expected last user turn kept")
	}
}

func TestTrimMessageDeterministicMode(t *testing.T) {
	cc := NewContextController("a", ".")
	cc.Options = &ContextOptions{MaxTurns: 1}
	cc.Messages = []communi.Message{
		{Role: keys.AgentRoleUser, Content: []*communi.ContentBlock{communi.NewTextContentBlock("old")}},
		{Role: keys.AgentRoleUser, Content: []*communi.ContentBlock{communi.NewTextContentBlock("new")}},
	}
	_ = cc.TrimMessage(context.Background(), &TrimMessageOptions{Mode: TrimModeDeterministic})
	if len(cc.Messages) != 1 {
		t.Fatalf("want 1 message, got %d", len(cc.Messages))
	}
}

type stubProvider struct {
	msg communi.Message
	err error
}

func (s *stubProvider) Name() string { return "stub" }

func (s *stubProvider) Stream(ctx context.Context, model llm.Model, message []communi.Message, opts *llm.Options) (<-chan communi.LLMEvent, error) {
	return nil, nil
}

func (s *stubProvider) Complete(ctx context.Context, model llm.Model, message []communi.Message, opts *llm.Options) (communi.Message, llm.Usage, error) {
	return s.msg, llm.Usage{}, s.err
}

func (s *stubProvider) Models() []llm.Model { return nil }

func TestCompactWithLLMForce(t *testing.T) {
	cc := NewContextController("a", ".")
	cc.Options = &ContextOptions{}
	mod := llm.Model{}
	mod.Provider = "openai"
	mod.ID = "gpt-4"
	cc.SetCompactLLM(&stubProvider{
		msg: communi.Message{
			Role: keys.AgentRoleAssistant,
			Content: []*communi.ContentBlock{
				communi.NewTextContentBlock("<analysis>x</analysis>\n<summary>done</summary>"),
			},
		},
	}, mod)
	cc.Messages = []communi.Message{
		{Role: keys.AgentRoleSystem, Content: []*communi.ContentBlock{communi.NewTextContentBlock("sys")}},
		{Role: keys.AgentRoleUser, Content: []*communi.ContentBlock{communi.NewTextContentBlock("hi")}},
	}
	err := cc.TrimMessage(context.Background(), &TrimMessageOptions{Mode: TrimModeLLMCompact, Force: true, TranscriptPath: "/tmp/t"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cc.Messages) != 2 {
		t.Fatalf("want system + summary user, got %d", len(cc.Messages))
	}
	txt := cc.Messages[1].ContentBlocksToText()
	if txt == "" || !containsFold(txt, "Summary:") {
		t.Fatalf("expected summary in compact output: %q", txt)
	}
	if !containsFold(txt, "/tmp/t") {
		t.Fatalf("expected transcript path in output")
	}
}

func containsFold(s, sub string) bool {
	return len(s) >= len(sub) && (indexFold(s, sub) >= 0)
}

func indexFold(s, sub string) int {
	// minimal substring check for test
	for i := 0; i+len(sub) <= len(s); i++ {
		match := true
		for j := 0; j < len(sub); j++ {
			c1, c2 := s[i+j], sub[j]
			if c1 >= 'A' && c1 <= 'Z' {
				c1 += 'a' - 'A'
			}
			if c2 >= 'A' && c2 <= 'Z' {
				c2 += 'a' - 'A'
			}
			if c1 != c2 {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

func TestShouldAutoCompactCooldown(t *testing.T) {
	cc := NewContextController("a", ".")
	mod := llm.Model{}
	mod.Provider = "p"
	mod.ID = "m"
	cc.SetCompactLLM(&stubProvider{msg: communi.NewAssistantMessage("1", "<summary>s</summary>")}, mod)
	cc.Options = &ContextOptions{
		AutoCompactMinUserTurns:       1,
		AutoCompactCooldownStreams:    2,
		AutoCompactMinEstimatedTokens: 0,
	}
	cc.Messages = []communi.Message{
		{Role: keys.AgentRoleSystem, Content: []*communi.ContentBlock{communi.NewTextContentBlock("sys")}},
		{Role: keys.AgentRoleUser, Content: []*communi.ContentBlock{communi.NewTextContentBlock("u")}},
	}
	// First stream: should compact (cooldown does not apply before any prior compact)
	if err := cc.TrimMessage(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if len(cc.Messages) != 2 || cc.streamsSinceLastCompact != 0 || !cc.everCompacted {
		t.Fatalf("expected compact (system+summary user), streams=%d msgs=%d ever=%v", cc.streamsSinceLastCompact, len(cc.Messages), cc.everCompacted)
	}
	// Refill messages for next tests (no auto compact while in cooldown)
	cc.Messages = []communi.Message{
		{Role: keys.AgentRoleSystem, Content: []*communi.ContentBlock{communi.NewTextContentBlock("sys")}},
		{Role: keys.AgentRoleUser, Content: []*communi.ContentBlock{communi.NewTextContentBlock("u")}},
	}
	_ = cc.TrimMessage(context.Background(), nil) // deterministic only, bump counter
	if cc.streamsSinceLastCompact != 1 {
		t.Fatalf("streams=%d", cc.streamsSinceLastCompact)
	}
	cc.Messages = []communi.Message{
		{Role: keys.AgentRoleSystem, Content: []*communi.ContentBlock{communi.NewTextContentBlock("sys")}},
		{Role: keys.AgentRoleUser, Content: []*communi.ContentBlock{communi.NewTextContentBlock("u")}},
	}
	_ = cc.TrimMessage(context.Background(), nil)
	if cc.streamsSinceLastCompact != 2 {
		t.Fatalf("streams=%d", cc.streamsSinceLastCompact)
	}
}
