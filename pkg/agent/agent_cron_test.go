package agent

import (
	"context"
	"testing"

	"github.com/vince-0202/acgo/pkg/communi"
	"github.com/vince-0202/acgo/pkg/config"
	"github.com/vince-0202/acgo/pkg/llm"
)

type okProvider struct{}

func (p *okProvider) Name() string { return "ok" }

func (p *okProvider) Stream(ctx context.Context, model llm.Model, message []communi.Message, opts *llm.Options) (<-chan communi.LLMEvent, error) {
	ch := make(chan communi.LLMEvent, 2)
	ch <- communi.LLMEvent{Type: communi.EventTextDelta, TextDelta: "done"}
	ch <- communi.LLMEvent{Type: communi.EventDone, StopReason: "stop"}
	close(ch)
	return ch, nil
}

func (p *okProvider) Complete(ctx context.Context, model llm.Model, message []communi.Message, opts *llm.Options) (communi.Message, llm.Usage, error) {
	return communi.NewAssistantMessage("complete", "done"), llm.Usage{}, nil
}

func (p *okProvider) Models() []llm.Model { return nil }

func TestPromptScheduledTask_ReturnsNilOnSuccess(t *testing.T) {
	ag := New(Options{
		ID:       "cron-test",
		WorkDir:  t.TempDir(),
		Provider: &okProvider{},
		Model: llm.Model{
			Provider:     "test",
			ModelSetting: config.ModelSetting{ID: "test-model"},
		},
	})

	if err := ag.PromptScheduledTask(context.Background(), "有一个会议"); err != nil {
		t.Fatalf("PromptScheduledTask should return nil on success, got: %v", err)
	}
}
