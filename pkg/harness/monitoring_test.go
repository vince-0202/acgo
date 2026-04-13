package harness

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/vince-0202/acgo/pkg/agent"
	"github.com/vince-0202/acgo/pkg/communi"
	"github.com/vince-0202/acgo/pkg/config"
	"github.com/vince-0202/acgo/pkg/llm"
)

type monitoringProvider struct {
	mu    sync.Mutex
	calls int
}

func (p *monitoringProvider) Name() string { return "monitoring-provider" }

func (p *monitoringProvider) Stream(ctx context.Context, model llm.Model, message []communi.Message, opts *llm.Options) (<-chan communi.LLMEvent, error) {
	p.mu.Lock()
	p.calls++
	callNum := p.calls
	p.mu.Unlock()

	ch := make(chan communi.LLMEvent, 4)
	switch callNum {
	case 1:
		ch <- communi.LLMEvent{
			Type: communi.EventToolCallStart,
			ToolCall: &communi.ToolCallRequest{
				ID:        "call-1",
				Name:      "monitor_echo",
				Arguments: json.RawMessage(`{"value":"ok"}`),
			},
		}
		ch <- communi.LLMEvent{
			Type: communi.EventToolCallEnd,
			ToolCall: &communi.ToolCallRequest{
				ID:        "call-1",
				Name:      "monitor_echo",
				Arguments: json.RawMessage(`{"value":"ok"}`),
			},
		}
		ch <- communi.LLMEvent{
			Type:       communi.EventDone,
			StopReason: "toolUse",
			Usage: &communi.Usage{
				InputTokens:  11,
				OutputTokens: 7,
				TotalTokens:  18,
			},
		}
	default:
		ch <- communi.LLMEvent{Type: communi.EventTextDelta, TextDelta: "done"}
		ch <- communi.LLMEvent{
			Type:       communi.EventDone,
			StopReason: "stop",
			Usage: &communi.Usage{
				InputTokens:  13,
				OutputTokens: 5,
				TotalTokens:  18,
			},
		}
	}
	close(ch)
	return ch, nil
}

func (p *monitoringProvider) Complete(ctx context.Context, model llm.Model, message []communi.Message, opts *llm.Options) (communi.Message, llm.Usage, error) {
	return communi.NewAssistantMessage("complete", "done"), llm.Usage{}, nil
}

func (p *monitoringProvider) Models() []llm.Model { return nil }

type monitoringEchoTool struct{}

func (t *monitoringEchoTool) Name() string        { return "monitor_echo" }
func (t *monitoringEchoTool) Label() string       { return "Monitor Echo" }
func (t *monitoringEchoTool) Description() string { return "echo tool for monitoring tests" }
func (t *monitoringEchoTool) JSONSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"value": map[string]any{"type": "string"},
		},
	}
}
func (t *monitoringEchoTool) Execute(ctx context.Context, toolCallID string, args json.RawMessage, update agent.ToolUpdateFunc) communi.ToolCallResult {
	return communi.NewToolCallResult(toolCallID, "ok")
}

func TestMonitoringController_WritesMetricsAndLLMLogs(t *testing.T) {
	dir := t.TempDir()
	metricsPath := filepath.Join(dir, "metrics.jsonl")
	llmPath := filepath.Join(dir, "llm.jsonl")

	ag := agent.New(agent.Options{
		ID:       "monitor-test",
		WorkDir:  dir,
		Provider: &monitoringProvider{},
		Model: llm.Model{
			Provider:     "test",
			ModelSetting: config.ModelSetting{ID: "test-model"},
		},
		Tools: []agent.Tool{&monitoringEchoTool{}},
	})

	h := NewHarness(NewMonitoringController(MonitoringOptions{
		MetricsFilePath: metricsPath,
		LLMFilePath:     llmPath,
	}))
	if err := h.Attach(ag); err != nil {
		t.Fatalf("attach monitoring harness: %v", err)
	}

	if err := h.Prompt(context.Background(), "run monitoring"); err != nil {
		t.Fatalf("prompt failed: %v", err)
	}

	metricsLines := readJSONLines(t, metricsPath)
	if len(metricsLines) < 2 {
		t.Fatalf("expected metrics lines, got %d", len(metricsLines))
	}
	var (
		foundTool bool
		foundTurn bool
	)
	for _, line := range metricsLines {
		switch line["type"] {
		case "tool_invocation":
			foundTool = true
		case "turn_metrics":
			foundTurn = true
			if got := int(line["tool_invocations"].(float64)); got != 1 {
				t.Fatalf("tool_invocations = %d, want 1", got)
			}
			if got := int(line["input_tokens"].(float64)); got != 24 {
				t.Fatalf("input_tokens = %d, want 24", got)
			}
			if got := int(line["output_tokens"].(float64)); got != 12 {
				t.Fatalf("output_tokens = %d, want 12", got)
			}
		}
	}
	if !foundTool {
		t.Fatal("expected tool_invocation metric entry")
	}
	if !foundTurn {
		t.Fatal("expected turn_metrics entry")
	}

	llmLines := readJSONLines(t, llmPath)
	if len(llmLines) != 2 {
		t.Fatalf("expected 2 llm log lines, got %d", len(llmLines))
	}
}

func readJSONLines(t *testing.T, path string) []map[string]any {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	rawLines := strings.Split(strings.TrimSpace(string(body)), "\n")
	out := make([]map[string]any, 0, len(rawLines))
	for _, raw := range rawLines {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		var line map[string]any
		if err := json.Unmarshal([]byte(raw), &line); err != nil {
			t.Fatalf("unmarshal line %q: %v", raw, err)
		}
		out = append(out, line)
	}
	return out
}
