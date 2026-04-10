package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/vince-0202/acgo/pkg/communi"
	"github.com/vince-0202/acgo/pkg/harness"
)

type flowStateTool struct {
	state *string
}

func (t *flowStateTool) Name() string  { return "flow_state" }
func (t *flowStateTool) Label() string { return "Flow State" }
func (t *flowStateTool) Description() string {
	return "set or read flow test state"
}
func (t *flowStateTool) JSONSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{"type": "string"},
			"value":  map[string]any{"type": "string"},
		},
		"required": []string{"action"},
	}
}
func (t *flowStateTool) Execute(ctx context.Context, toolCallID string, args json.RawMessage, update harness.ToolUpdateFunc) communi.ToolCallResult {
	var req struct {
		Action string `json:"action"`
		Value  string `json:"value"`
	}
	if err := json.Unmarshal(args, &req); err != nil {
		return communi.ErrorToolCallResult(toolCallID, err)
	}
	switch req.Action {
	case "set":
		*t.state = req.Value
		return communi.NewToolCallResult(toolCallID, "set:"+req.Value)
	case "get":
		return communi.NewToolCallResult(toolCallID, "get:"+*t.state)
	default:
		return communi.ErrorToolCallResult(toolCallID, fmt.Errorf("unknown action"))
	}
}

type flowFailTool struct{}

func (t *flowFailTool) Name() string        { return "flow_fail" }
func (t *flowFailTool) Label() string       { return "Flow Fail" }
func (t *flowFailTool) Description() string { return "always fails" }
func (t *flowFailTool) JSONSchema() map[string]any {
	return map[string]any{"type": "object"}
}
func (t *flowFailTool) Execute(ctx context.Context, toolCallID string, args json.RawMessage, update harness.ToolUpdateFunc) communi.ToolCallResult {
	return communi.ErrorToolCallResult(toolCallID, fmt.Errorf("forced failure"))
}

func TestToolFlow_SequentialSteps(t *testing.T) {
	state := ""
	tc := harness.NewToolController("test-agent", nil, nil, nil,
		NewToolFlowTool(),
		&flowStateTool{state: &state},
	)
	args := []byte(`{
		"steps":[
			{"id":"s1","tool":"flow_state","args":{"action":"set","value":"abc"}},
			{"id":"s2","tool":"flow_state","args":{"action":"get"}}
		]
	}`)
	res, err := tc.ExecuteByName(context.Background(), "flow-call", "tool_flow", args, harness.ExecuteToolOptions{
		AppendTranscript: false,
		EmitEvents:       false,
	})
	if err != nil {
		t.Fatalf("tool_flow execute error: %v", err)
	}
	if res.IsError() {
		t.Fatalf("tool_flow result error: %v", res.Error)
	}
	var report struct {
		Status         string `json:"status"`
		CompletedSteps int    `json:"completed_steps"`
		FailedSteps    int    `json:"failed_steps"`
		Steps          []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Output string `json:"output"`
		} `json:"steps"`
	}
	if err := json.Unmarshal([]byte(toolResultToText(res)), &report); err != nil {
		t.Fatalf("unmarshal flow report: %v", err)
	}
	if report.Status != "completed" || report.CompletedSteps != 2 || report.FailedSteps != 0 {
		t.Fatalf("unexpected flow status: %#v", report)
	}
	if len(report.Steps) != 2 || report.Steps[1].Output != "get:abc" {
		t.Fatalf("unexpected step outputs: %#v", report.Steps)
	}
}

func TestToolFlow_StopOnError(t *testing.T) {
	state := ""
	tc := harness.NewToolController("test-agent", nil, nil, nil,
		NewToolFlowTool(),
		&flowStateTool{state: &state},
		&flowFailTool{},
	)
	args := []byte(`{
		"steps":[
			{"tool":"flow_fail","args":{}},
			{"tool":"flow_state","args":{"action":"set","value":"should-not-run"}}
		]
	}`)
	res, err := tc.ExecuteByName(context.Background(), "flow-call", "tool_flow", args, harness.ExecuteToolOptions{
		AppendTranscript: false,
		EmitEvents:       false,
	})
	if err != nil {
		t.Fatalf("tool_flow should return JSON report, not a top-level error: %v", err)
	}
	if res.IsError() {
		t.Fatalf("unexpected tool_flow error result: %v", res.Error)
	}
	var report struct {
		Status         string `json:"status"`
		CompletedSteps int    `json:"completed_steps"`
		FailedSteps    int    `json:"failed_steps"`
		StoppedAtStep  int    `json:"stopped_at_step"`
		Steps          []struct {
			Status string `json:"status"`
		} `json:"steps"`
	}
	if err := json.Unmarshal([]byte(toolResultToText(res)), &report); err != nil {
		t.Fatalf("unmarshal flow report: %v", err)
	}
	if report.Status != "failed" || report.CompletedSteps != 0 || report.FailedSteps != 1 || report.StoppedAtStep != 1 {
		t.Fatalf("unexpected flow report: %#v", report)
	}
	if len(report.Steps) != 1 || report.Steps[0].Status != "failed" {
		t.Fatalf("unexpected steps: %#v", report.Steps)
	}
	if state != "" {
		t.Fatalf("state changed unexpectedly: %q", state)
	}
}

func TestExecuteByName_NoTranscriptContextRequiredWhenDisabled(t *testing.T) {
	tc := harness.NewToolController("test-agent", nil, nil, nil, &flowFailTool{})
	_, err := tc.ExecuteByName(context.Background(), "call", "flow_fail", []byte(`{}`), harness.ExecuteToolOptions{
		AppendTranscript: false,
		EmitEvents:       false,
	})
	if err == nil {
		t.Fatal("expected tool error")
	}
}
