package agent

import (
	"context"
	"encoding/json"
	"github.com/vince-0202/acgo/pkg/keys"
	"testing"

	"github.com/vince-0202/acgo/pkg/config"
	"github.com/vince-0202/acgo/pkg/llm"
)

// recordingTool is a test AgentTool that records the last received arguments.
type recordingTool struct {
	name         string
	receivedArgs json.RawMessage
	schema       map[string]any // if set, used instead of the default read-file schema
}

func (t *recordingTool) Name() string {
	if t.name != "" {
		return t.name
	}
	return "read"
}
func (t *recordingTool) Label() string { return "Read file" }
func (t *recordingTool) Description() string {
	return "Read contents of a file"
}
func (t *recordingTool) JSONSchema() map[string]any {
	if t.schema != nil {
		return t.schema
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string", "description": "File path"},
		},
		"required": []any{"path"},
	}
}
func (t *recordingTool) Execute(ctx context.Context, toolCallID string, args json.RawMessage, _ ToolUpdateFunc) (ToolResult, error) {
	t.receivedArgs = append(json.RawMessage(nil), args...)
	return ToolResult{Content: "ok", IsError: false}, nil
}

func TestExecutePendingTools_NormalizesDoubleEncodedArguments(t *testing.T) {
	rec := &recordingTool{name: "read"}
	// Double-encoded: model returns arguments as a JSON string containing the real JSON.
	doubleEncoded := `"{\"path\":\"/tmp/foo\"}"`
	callCount := 0

	mockStream := func(_ llm.Context, _ llm.Model, _ *llm.Options) (<-chan llm.Event, error) {
		callCount++
		ch := make(chan llm.Event, 8)
		if callCount == 1 {
			// First turn: model requests a tool call.
			ch <- llm.Event{Type: llm.EventStart}
			ch <- llm.Event{Type: llm.EventToolCallStart, ToolCall: &llm.ToolCall{
				ID:        "call-1",
				Name:      "read",
				Arguments: json.RawMessage(doubleEncoded),
			}}
			ch <- llm.Event{Type: llm.EventToolCallEnd, ToolCall: &llm.ToolCall{
				ID:        "call-1",
				Name:      "read",
				Arguments: json.RawMessage(doubleEncoded),
			}}
			ch <- llm.Event{Type: llm.EventDone, StopReason: "toolUse"}
		} else {
			// Second turn (after tool result): model responds with text and stops.
			ch <- llm.Event{Type: llm.EventStart}
			ch <- llm.Event{Type: llm.EventTextStart}
			ch <- llm.Event{Type: llm.EventTextDelta, TextDelta: "Done."}
			ch <- llm.Event{Type: llm.EventTextEnd}
			ch <- llm.Event{Type: llm.EventDone, StopReason: "stop"}
		}
		close(ch)
		return ch, nil
	}

	a := New("test", Options{
		InitialState: State{
			Model: llm.Model{ModelSetting: config.ModelSetting{ID: "test-model"}, Provider: "test"},
			Tools: []AgentTool{rec},
		},
		StreamFn: mockStream,
	})
	ctx := context.Background()
	if err := a.Prompt(ctx, "read /tmp/foo"); err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	want := `{"path":"/tmp/foo"}`
	if string(rec.receivedArgs) != want {
		t.Errorf("tool received args = %q, want %q", string(rec.receivedArgs), want)
	}
}

func TestExecutePendingTools_EmptyArgumentsBecomeEmptyObject(t *testing.T) {
	// Schema allows {} so we exercise Normalize + Coerce + Validate and still call Execute.
	rec := &recordingTool{
		name: "write",
		schema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}
	callCount := 0

	mockStream := func(_ llm.Context, _ llm.Model, _ *llm.Options) (<-chan llm.Event, error) {
		callCount++
		ch := make(chan llm.Event, 8)
		if callCount == 1 {
			ch <- llm.Event{Type: llm.EventStart}
			ch <- llm.Event{Type: llm.EventToolCallStart, ToolCall: &llm.ToolCall{
				ID:        "call-2",
				Name:      "write",
				Arguments: nil,
			}}
			ch <- llm.Event{Type: llm.EventToolCallEnd, ToolCall: &llm.ToolCall{
				ID:        "call-2",
				Name:      "write",
				Arguments: nil,
			}}
			ch <- llm.Event{Type: llm.EventDone, StopReason: "toolUse"}
		} else {
			ch <- llm.Event{Type: llm.EventStart}
			ch <- llm.Event{Type: llm.EventTextStart}
			ch <- llm.Event{Type: llm.EventTextDelta, TextDelta: "Done."}
			ch <- llm.Event{Type: llm.EventTextEnd}
			ch <- llm.Event{Type: llm.EventDone, StopReason: "stop"}
		}
		close(ch)
		return ch, nil
	}

	a := New("test", Options{
		InitialState: State{
			Model: llm.Model{ModelSetting: config.ModelSetting{ID: "test-model"}, Provider: "test"},
			Tools: []AgentTool{rec},
		},
		StreamFn: mockStream,
	})
	ctx := context.Background()
	if err := a.Prompt(ctx, "write something"); err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	want := `{}`
	if string(rec.receivedArgs) != want {
		t.Errorf("tool received args = %q, want %q (empty args should normalize to {})", string(rec.receivedArgs), want)
	}
}

func TestPrompt_DrainsSteeringThenFollowUpQueues(t *testing.T) {
	callCount := 0
	mockStream := func(_ llm.Context, _ llm.Model, _ *llm.Options) (<-chan llm.Event, error) {
		callCount++
		ch := make(chan llm.Event, 8)
		ch <- llm.Event{Type: llm.EventStart}
		ch <- llm.Event{Type: llm.EventTextStart}
		ch <- llm.Event{Type: llm.EventTextDelta, TextDelta: "ok"}
		ch <- llm.Event{Type: llm.EventTextEnd}
		ch <- llm.Event{Type: llm.EventDone, StopReason: "stop"}
		close(ch)
		return ch, nil
	}
	a := New("test", Options{
		InitialState: State{
			Model: llm.Model{ModelSetting: config.ModelSetting{ID: "test-model"}, Provider: "test"},
			Tools: nil,
		},
		StreamFn: mockStream,
	})
	a.EnqueueFollowUp(Message{ID: "follow-1", Role: keys.AgentRoleUser, Content: "follow-up"})
	a.EnqueueSteering(Message{ID: "steer-1", Role: keys.AgentRoleUser, Content: "steering"})
	ctx := context.Background()
	if err := a.Prompt(ctx, "first"); err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	state := a.State()
	// Order: first (initial), then steering (consumed first), then follow-up.
	var userContents []string
	for _, m := range state.Messages {
		if m.Role == keys.AgentRoleUser {
			userContents = append(userContents, m.Content)
		}
	}
	wantContents := []string{"first", "steering", "follow-up"}
	if len(userContents) != len(wantContents) {
		t.Errorf("user messages: %v, want %v", userContents, wantContents)
	} else {
		for i := range wantContents {
			if userContents[i] != wantContents[i] {
				t.Errorf("user message[%d] = %q, want %q", i, userContents[i], wantContents[i])
			}
		}
	}
	if callCount != 3 {
		t.Errorf("stream called %d times, want 3 (first, steering, follow-up)", callCount)
	}
}

func TestReplaceMessages(t *testing.T) {
	a := New("test", Options{InitialState: State{}})
	a.AppendMessage(Message{Role: keys.AgentRoleUser, Content: "a"})
	a.AppendMessage(Message{Role: keys.AgentRoleAssistant, Content: "b"})
	replacement := []Message{
		{Role: keys.AgentRoleUser, Content: "x"},
		{Role: keys.AgentRoleAssistant, Content: "y"},
	}
	a.ReplaceMessages(replacement)
	state := a.State()
	if len(state.Messages) != 2 || state.Messages[0].Content != "x" || state.Messages[1].Content != "y" {
		t.Errorf("ReplaceMessages: got %v", state.Messages)
	}
	// Caller mutating slice after ReplaceMessages should not affect agent
	replacement[0].Content = "z"
	if a.State().Messages[0].Content != "x" {
		t.Errorf("ReplaceMessages should copy; agent has %q", a.State().Messages[0].Content)
	}
	a.ReplaceMessages(nil)
	if len(a.State().Messages) != 0 {
		t.Errorf("ReplaceMessages(nil): got %d messages", len(a.State().Messages))
	}
}

func TestSetErrorAndClearError(t *testing.T) {
	a := New("test", Options{InitialState: State{}})
	if a.State().Error != nil {
		t.Fatal("initial error should be nil")
	}
	err := context.DeadlineExceeded
	a.SetError(err)
	if a.State().Error != err {
		t.Errorf("SetError: got %v", a.State().Error)
	}
	a.ClearError()
	if a.State().Error != nil {
		t.Errorf("ClearError: got %v", a.State().Error)
	}
}

func TestWaitForIdle(t *testing.T) {
	a := New("test", Options{InitialState: State{}})
	ctx := context.Background()
	if err := a.WaitForIdle(ctx); err != nil {
		t.Errorf("WaitForIdle when idle: %v", err)
	}
	// When already idle, WaitForIdle returns nil even if contextController is cancelled (idle check is first).
	ctxDone, cancel := context.WithCancel(context.Background())
	cancel()
	if err := a.WaitForIdle(ctxDone); err != nil {
		t.Errorf("WaitForIdle when idle with cancelled contextController: %v", err)
	}
}
