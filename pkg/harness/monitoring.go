package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/vince-0202/acgo/pkg/agent"
	"github.com/vince-0202/acgo/pkg/communi"
)

const (
	defaultMonitoringDir         = "monitoring"
	defaultMetricsFileName       = "metrics.jsonl"
	defaultLLMTranscriptFileName = "llm.jsonl"
)

type MonitoringOptions struct {
	MetricsFilePath string
	LLMFilePath     string
}

type MonitoringController struct {
	opts MonitoringOptions

	mu        sync.Mutex
	agentID   string
	metrics   string
	llm       string
	turns     map[string]*turnMonitoringState
	llmSeqNum map[string]int
}

type turnMonitoringState struct {
	TurnID          string
	StartedAt       time.Time
	FirstTokenAt    time.Time
	ModelLatency    time.Duration
	InputTokens     int
	OutputTokens    int
	TotalTokens     int
	ToolInvocations []*toolInvocationMetric
	LLMCalls        map[int]*llmCallState
}

type llmCallState struct {
	Seq          int
	StartedAt    time.Time
	FinishedAt   time.Time
	Logged       bool
	Request      []monitorMessage `json:"request"`
	Response     monitorMessage   `json:"response"`
	StopReason   string           `json:"stop_reason,omitempty"`
	InputTokens  int              `json:"input_tokens,omitempty"`
	OutputTokens int              `json:"output_tokens,omitempty"`
	TotalTokens  int              `json:"total_tokens,omitempty"`
	Error        string           `json:"error,omitempty"`
}

type toolInvocationMetric struct {
	ToolCallID string    `json:"tool_call_id"`
	ToolName   string    `json:"tool_name"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	DurationMs int64     `json:"duration_ms"`
	Success    bool      `json:"success"`
	Error      string    `json:"error,omitempty"`
}

type monitorMessage struct {
	Role      string                    `json:"role,omitempty"`
	Text      string                    `json:"text,omitempty"`
	Thinking  string                    `json:"thinking,omitempty"`
	IsError   bool                      `json:"is_error,omitempty"`
	Metadata  map[string]any            `json:"metadata,omitempty"`
	ToolCall  *communi.ToolCallRequest  `json:"tool_call,omitempty"`
	ToolCalls []communi.ToolCallRequest `json:"tool_calls,omitempty"`
}

func NewMonitoringController(opts MonitoringOptions) *MonitoringController {
	return &MonitoringController{
		opts:      opts,
		turns:     make(map[string]*turnMonitoringState),
		llmSeqNum: make(map[string]int),
	}
}

func (mc *MonitoringController) Name() string {
	return "monitoring"
}

func (mc *MonitoringController) Install(runtime agent.AgentRuntime) (func(), error) {
	mc.mu.Lock()
	mc.agentID = runtime.ID()
	mc.resolvePaths(runtime.ContextManager().WorkDir())
	mc.mu.Unlock()

	if err := mc.ensureFiles(); err != nil {
		return nil, err
	}

	unsub := runtime.Subscribe(func(event agent.Event, abort func()) {
		mc.handleEvent(event)
	})
	return func() {
		unsub()
		mc.mu.Lock()
		defer mc.mu.Unlock()
		mc.turns = make(map[string]*turnMonitoringState)
		mc.llmSeqNum = make(map[string]int)
	}, nil
}

func (mc *MonitoringController) handleEvent(event agent.Event) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	switch event.Type {
	case agent.EventTurnStart:
		mc.onTurnStart(event)
	case agent.EventBeforeLLMCall:
		mc.onBeforeLLMCall(event)
	case agent.EventMessageUpdate:
		mc.onMessageUpdate(event)
	case agent.EventAfterLLMCall:
		mc.onAfterLLMCall(event)
	case agent.EventToolExecutionStart:
		mc.onToolExecutionStart(event)
	case agent.EventToolExecutionEnd:
		mc.onToolExecutionEnd(event)
	case agent.EventTurnEnd:
		mc.onTurnEnd(event)
	}
}

func (mc *MonitoringController) onTurnStart(event agent.Event) {
	turnID := strings.TrimSpace(event.TurnID)
	if turnID == "" {
		return
	}
	mc.turns[turnID] = &turnMonitoringState{
		TurnID:          turnID,
		StartedAt:       time.Now(),
		ToolInvocations: make([]*toolInvocationMetric, 0),
		LLMCalls:        make(map[int]*llmCallState),
	}
	mc.llmSeqNum[turnID] = 0
}

func (mc *MonitoringController) onBeforeLLMCall(event agent.Event) {
	turn := mc.turnState(event.TurnID)
	if turn == nil || event.Agent == nil {
		return
	}
	mc.llmSeqNum[event.TurnID]++
	seq := mc.llmSeqNum[event.TurnID]
	turn.LLMCalls[seq] = &llmCallState{
		Seq:       seq,
		StartedAt: time.Now(),
		Request:   snapshotMessages(event.Agent.ContextManager().MessageSnapshot()),
	}
}

func (mc *MonitoringController) onMessageUpdate(event agent.Event) {
	turn := mc.turnState(event.TurnID)
	if turn == nil || !turn.FirstTokenAt.IsZero() {
		return
	}
	if event.LlmEvent == nil {
		return
	}
	switch event.LlmEvent.Type {
	case communi.EventTextDelta, communi.EventThinkingDelta, communi.EventToolCallStart, communi.EventToolCallDelta, communi.EventToolCallEnd:
		turn.FirstTokenAt = time.Now()
	}
}

func (mc *MonitoringController) onAfterLLMCall(event agent.Event) {
	turn := mc.turnState(event.TurnID)
	if turn == nil {
		return
	}
	seq := mc.llmSeqNum[event.TurnID]
	call := turn.LLMCalls[seq]
	if call == nil {
		return
	}
	finishedAt := time.Now()
	call.FinishedAt = finishedAt
	call.Logged = true
	call.Response = messageToMonitorMessage(event.Message)
	if event.LlmEvent != nil {
		call.StopReason = strings.TrimSpace(event.LlmEvent.StopReason)
		if event.LlmEvent.Usage != nil {
			call.InputTokens = event.LlmEvent.Usage.InputTokens
			call.OutputTokens = event.LlmEvent.Usage.OutputTokens
			call.TotalTokens = normalizeTotalTokens(event.LlmEvent.Usage)
			turn.InputTokens += call.InputTokens
			turn.OutputTokens += call.OutputTokens
			turn.TotalTokens += call.TotalTokens
		}
	}
	if event.Error != nil {
		call.Error = event.Error.Error()
	}
	turn.ModelLatency += finishedAt.Sub(call.StartedAt)
	_ = mc.appendJSONLine(mc.llm, map[string]any{
		"type":          "llm_call",
		"agent_id":      mc.agentID,
		"turn_id":       event.TurnID,
		"sequence":      call.Seq,
		"started_at":    call.StartedAt.UTC().Format(time.RFC3339Nano),
		"finished_at":   finishedAt.UTC().Format(time.RFC3339Nano),
		"latency_ms":    finishedAt.Sub(call.StartedAt).Milliseconds(),
		"stop_reason":   call.StopReason,
		"input_tokens":  call.InputTokens,
		"output_tokens": call.OutputTokens,
		"total_tokens":  call.TotalTokens,
		"request":       call.Request,
		"response":      call.Response,
		"error":         call.Error,
	})
}

func (mc *MonitoringController) onToolExecutionStart(event agent.Event) {
	turn := mc.turnState(event.TurnID)
	if turn == nil || event.Tool == nil {
		return
	}
	turn.ToolInvocations = append(turn.ToolInvocations, &toolInvocationMetric{
		ToolCallID: strings.TrimSpace(event.ToolCallID),
		ToolName:   strings.TrimSpace(event.Tool.Name()),
		StartedAt:  time.Now(),
	})
}

func (mc *MonitoringController) onToolExecutionEnd(event agent.Event) {
	turn := mc.turnState(event.TurnID)
	if turn == nil {
		return
	}
	for i := len(turn.ToolInvocations) - 1; i >= 0; i-- {
		item := turn.ToolInvocations[i]
		if item == nil || !item.FinishedAt.IsZero() {
			continue
		}
		if item.ToolCallID != strings.TrimSpace(event.ToolCallID) {
			continue
		}
		item.FinishedAt = time.Now()
		item.DurationMs = item.FinishedAt.Sub(item.StartedAt).Milliseconds()
		item.Success = event.Error == nil
		if event.Error != nil {
			item.Error = event.Error.Error()
		}
		_ = mc.appendJSONLine(mc.metrics, map[string]any{
			"type":         "tool_invocation",
			"agent_id":     mc.agentID,
			"turn_id":      event.TurnID,
			"tool_call_id": item.ToolCallID,
			"tool_name":    item.ToolName,
			"started_at":   item.StartedAt.UTC().Format(time.RFC3339Nano),
			"finished_at":  item.FinishedAt.UTC().Format(time.RFC3339Nano),
			"duration_ms":  item.DurationMs,
			"success":      item.Success,
			"error":        item.Error,
		})
		return
	}
}

func (mc *MonitoringController) onTurnEnd(event agent.Event) {
	turn := mc.turnState(event.TurnID)
	if turn == nil {
		return
	}
	if event.Message != nil && len(event.Message.ToolCalls) > 0 && event.Error == nil {
		return
	}
	finishedAt := time.Now()

	// Flush failed LLM call if stream setup failed before EventAfterLLMCall.
	seq := mc.llmSeqNum[event.TurnID]
	if seq > 0 {
		if call := turn.LLMCalls[seq]; call != nil && !call.Logged && event.Error != nil {
			call.Error = event.Error.Error()
			call.FinishedAt = finishedAt
			call.Logged = true
			_ = mc.appendJSONLine(mc.llm, map[string]any{
				"type":        "llm_call",
				"agent_id":    mc.agentID,
				"turn_id":     event.TurnID,
				"sequence":    call.Seq,
				"started_at":  call.StartedAt.UTC().Format(time.RFC3339Nano),
				"finished_at": call.FinishedAt.UTC().Format(time.RFC3339Nano),
				"latency_ms":  call.FinishedAt.Sub(call.StartedAt).Milliseconds(),
				"request":     call.Request,
				"response":    call.Response,
				"error":       call.Error,
			})
		}
	}

	ttftMs := int64(0)
	if !turn.FirstTokenAt.IsZero() {
		ttftMs = turn.FirstTokenAt.Sub(turn.StartedAt).Milliseconds()
	}

	totalTimeMs := finishedAt.Sub(turn.StartedAt).Milliseconds()
	toolSummary := summarizeTools(turn.ToolInvocations)
	_ = mc.appendJSONLine(mc.metrics, map[string]any{
		"type":               "turn_metrics",
		"agent_id":           mc.agentID,
		"turn_id":            event.TurnID,
		"started_at":         turn.StartedAt.UTC().Format(time.RFC3339Nano),
		"finished_at":        finishedAt.UTC().Format(time.RFC3339Nano),
		"total_time_ms":      totalTimeMs,
		"ttft_ms":            ttftMs,
		"model_latency_ms":   turn.ModelLatency.Milliseconds(),
		"input_tokens":       turn.InputTokens,
		"output_tokens":      turn.OutputTokens,
		"total_tokens":       turn.TotalTokens,
		"tool_invocations":   toolSummary.invocationCount,
		"tool_success_count": toolSummary.successCount,
		"tool_failure_count": toolSummary.failureCount,
		"tool_avg_time_ms":   toolSummary.avgDurationMs,
		"tool_peak_time_ms":  toolSummary.peakDurationMs,
		"tool_success_rate":  toolSummary.successRate,
		"tools":              turn.ToolInvocations,
		"error":              errorString(event.Error),
	})

	delete(mc.turns, event.TurnID)
	delete(mc.llmSeqNum, event.TurnID)
}

func (mc *MonitoringController) turnState(turnID string) *turnMonitoringState {
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return nil
	}
	return mc.turns[turnID]
}

func (mc *MonitoringController) resolvePaths(workDir string) {
	baseDir := filepath.Join(strings.TrimSpace(workDir), defaultMonitoringDir)
	mc.metrics = strings.TrimSpace(mc.opts.MetricsFilePath)
	if mc.metrics == "" {
		mc.metrics = filepath.Join(baseDir, defaultMetricsFileName)
	}
	mc.llm = strings.TrimSpace(mc.opts.LLMFilePath)
	if mc.llm == "" {
		mc.llm = filepath.Join(baseDir, defaultLLMTranscriptFileName)
	}
}

func (mc *MonitoringController) ensureFiles() error {
	for _, path := range []string{mc.metrics, mc.llm} {
		if strings.TrimSpace(path) == "" {
			return fmt.Errorf("monitoring file path is empty")
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		f, err := os.OpenFile(path, os.O_CREATE, 0644)
		if err != nil {
			return err
		}
		_ = f.Close()
	}
	return nil
}

func (mc *MonitoringController) appendJSONLine(path string, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(b, '\n')); err != nil {
		return err
	}
	return nil
}

type toolAggregate struct {
	invocationCount int
	successCount    int
	failureCount    int
	avgDurationMs   float64
	peakDurationMs  int64
	successRate     float64
}

func summarizeTools(items []*toolInvocationMetric) toolAggregate {
	var out toolAggregate
	var totalDuration int64
	for _, item := range items {
		if item == nil || item.FinishedAt.IsZero() {
			continue
		}
		out.invocationCount++
		totalDuration += item.DurationMs
		if item.DurationMs > out.peakDurationMs {
			out.peakDurationMs = item.DurationMs
		}
		if item.Success {
			out.successCount++
		} else {
			out.failureCount++
		}
	}
	if out.invocationCount > 0 {
		out.avgDurationMs = float64(totalDuration) / float64(out.invocationCount)
		out.successRate = float64(out.successCount) / float64(out.invocationCount)
	}
	return out
}

func normalizeTotalTokens(usage *communi.Usage) int {
	if usage == nil {
		return 0
	}
	if usage.TotalTokens > 0 {
		return usage.TotalTokens
	}
	return usage.InputTokens + usage.OutputTokens
}

func snapshotMessages(messages []communi.Message) []monitorMessage {
	out := make([]monitorMessage, 0, len(messages))
	for _, msg := range messages {
		out = append(out, messageToMonitorMessage(&msg))
	}
	return out
}

func messageToMonitorMessage(msg *communi.Message) monitorMessage {
	if msg == nil {
		return monitorMessage{}
	}
	res := monitorMessage{
		Role:     string(msg.Role),
		Text:     msg.ContentBlocksToText(),
		Thinking: msg.Thinking,
		IsError:  msg.IsError,
		Metadata: msg.Metadata,
	}
	if msg.ToolCall != nil {
		toolCall := *msg.ToolCall
		res.ToolCall = &toolCall
	}
	if len(msg.ToolCalls) > 0 {
		res.ToolCalls = append([]communi.ToolCallRequest(nil), msg.ToolCalls...)
	}
	return res
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
