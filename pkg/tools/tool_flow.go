package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/vince-0202/acgo/pkg/agent"
	"strings"
	"time"

	"github.com/vince-0202/acgo/pkg/communi"
)

const maxToolFlowSteps = 20

type toolFlowTool struct{}

func (t *toolFlowTool) Name() string  { return "tool_flow" }
func (t *toolFlowTool) Label() string { return "Tool Flow" }
func (t *toolFlowTool) Description() string {
	return "Execute an LLM-defined sequence of tool calls inside one tool invocation. Use steps [{tool,args,id?}] for ordered orchestration."
}

func (t *toolFlowTool) JSONSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"steps": map[string]any{
				"type":        "array",
				"description": "Ordered workflow steps. Each step runs after the previous one completes.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id": map[string]any{
							"type":        "string",
							"description": "Optional caller-provided step identifier.",
						},
						"tool": map[string]any{
							"type":        "string",
							"minLength":   1,
							"description": "Tool name to execute.",
						},
						"args": map[string]any{
							"description": "Arguments payload passed to target tool.",
						},
					},
					"required": []string{"tool"},
				},
				"minItems": 1,
			},
			"continue_on_error": map[string]any{
				"type":        "boolean",
				"description": "When true, continue remaining steps even if one step fails.",
			},
		},
		"required": []string{"steps"},
	}
}

func (t *toolFlowTool) Execute(ctx context.Context, toolCallID string, args json.RawMessage, update agent.ToolUpdateFunc) communi.ToolCallResult {
	executor, ok := agent.ToolExecutorFromContext(ctx)
	if !ok {
		return communi.ErrorToolCallResult(toolCallID, fmt.Errorf("tool_flow executor is unavailable"))
	}

	var req struct {
		Steps []struct {
			ID   string          `json:"id"`
			Tool string          `json:"tool"`
			Args json.RawMessage `json:"args"`
		} `json:"steps"`
		ContinueOnError bool `json:"continue_on_error"`
	}
	if err := json.Unmarshal(args, &req); err != nil {
		return communi.ErrorToolCallResult(toolCallID, err)
	}
	if len(req.Steps) == 0 {
		return communi.ErrorToolCallResult(toolCallID, fmt.Errorf("steps cannot be empty"))
	}
	if len(req.Steps) > maxToolFlowSteps {
		return communi.ErrorToolCallResult(toolCallID, fmt.Errorf("too many steps: got %d, max %d", len(req.Steps), maxToolFlowSteps))
	}

	type stepResult struct {
		Index      int    `json:"index"`
		ID         string `json:"id,omitempty"`
		Tool       string `json:"tool"`
		Status     string `json:"status"`
		DurationMs int64  `json:"duration_ms"`
		Output     string `json:"output,omitempty"`
		Error      string `json:"error,omitempty"`
	}
	report := struct {
		Status          string       `json:"status"`
		ContinueOnError bool         `json:"continue_on_error"`
		TotalSteps      int          `json:"total_steps"`
		CompletedSteps  int          `json:"completed_steps"`
		FailedSteps     int          `json:"failed_steps"`
		StoppedAtStep   int          `json:"stopped_at_step,omitempty"`
		Steps           []stepResult `json:"steps"`
	}{
		Status:          "completed",
		ContinueOnError: req.ContinueOnError,
		TotalSteps:      len(req.Steps),
		Steps:           make([]stepResult, 0, len(req.Steps)),
	}

	for i, step := range req.Steps {
		toolName := strings.TrimSpace(step.Tool)
		if toolName == "" {
			report.Status = "failed"
			report.FailedSteps++
			report.StoppedAtStep = i + 1
			report.Steps = append(report.Steps, stepResult{
				Index:  i + 1,
				ID:     strings.TrimSpace(step.ID),
				Tool:   toolName,
				Status: "failed",
				Error:  "tool name is required",
			})
			if !req.ContinueOnError {
				break
			}
			continue
		}
		if toolName == t.Name() {
			report.Status = "failed"
			report.FailedSteps++
			report.StoppedAtStep = i + 1
			report.Steps = append(report.Steps, stepResult{
				Index:  i + 1,
				ID:     strings.TrimSpace(step.ID),
				Tool:   toolName,
				Status: "failed",
				Error:  "tool_flow cannot invoke itself",
			})
			if !req.ContinueOnError {
				break
			}
			continue
		}

		stepArgs := step.Args
		if len(stepArgs) == 0 {
			stepArgs = json.RawMessage(`{}`)
		}
		callID := fmt.Sprintf("%s-step-%d", toolCallID, i+1)
		start := time.Now()
		res, err := executor.ExecuteByName(ctx, callID, toolName, stepArgs, agent.ExecuteToolOptions{
			AppendTranscript: false,
			EmitEvents:       false,
		})
		durationMs := time.Since(start).Milliseconds()

		sr := stepResult{
			Index:      i + 1,
			ID:         strings.TrimSpace(step.ID),
			Tool:       toolName,
			DurationMs: durationMs,
		}
		if err != nil || res.IsError() {
			report.Status = "failed"
			report.FailedSteps++
			report.StoppedAtStep = i + 1
			sr.Status = "failed"
			if err != nil {
				sr.Error = err.Error()
			} else if res.Error != nil {
				sr.Error = res.Error.Error()
			}
			sr.Output = toolResultToText(res)
			report.Steps = append(report.Steps, sr)
			if !req.ContinueOnError {
				break
			}
			continue
		}
		report.CompletedSteps++
		sr.Status = "completed"
		sr.Output = toolResultToText(res)
		report.Steps = append(report.Steps, sr)
	}

	body, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return communi.ErrorToolCallResult(toolCallID, err)
	}
	return communi.NewToolCallResult(toolCallID, string(body))
}

func toolResultToText(res communi.ToolCallResult) string {
	if len(res.Content) == 0 {
		if res.Error != nil {
			return res.Error.Error()
		}
		return ""
	}
	var b strings.Builder
	for _, c := range res.Content {
		if c == nil || c.Type != "text" || c.Text == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(c.Text)
	}
	if b.Len() == 0 && res.Error != nil {
		return res.Error.Error()
	}
	return strings.TrimSpace(b.String())
}

func NewToolFlowTool() agent.Tool {
	return &toolFlowTool{}
}
