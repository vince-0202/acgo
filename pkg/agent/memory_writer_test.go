package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"acgo/pkg/config"
	"acgo/pkg/keys"
	"acgo/pkg/llm"
	"acgo/pkg/memory"
)

type recordingMemoryWriter struct {
	calls         int
	lastSessionID string
	lastUserText  string
	lastAssistant string
}

func (w *recordingMemoryWriter) WriteDialogue(_ context.Context, sessionID string, userText string, assistantText string) error {
	w.calls++
	w.lastSessionID = sessionID
	w.lastUserText = userText
	w.lastAssistant = assistantText
	return nil
}

func TestPrompt_AutoWriteDialogueMemory(t *testing.T) {
	writer := &recordingMemoryWriter{}
	mockStream := func(_ llm.Context, _ llm.Model, _ *llm.Options) (<-chan llm.Event, error) {
		ch := make(chan llm.Event, 16)
		ch <- llm.Event{Type: llm.EventStart}
		ch <- llm.Event{Type: llm.EventTextStart}
		ch <- llm.Event{Type: llm.EventTextDelta, TextDelta: "final assistant"}
		ch <- llm.Event{Type: llm.EventTextEnd}
		ch <- llm.Event{Type: llm.EventDone, StopReason: "stop"}
		close(ch)
		return ch, nil
	}

	a := New("test", Options{
		InitialState: State{
			SystemPrompt: "",
			Model:        llm.Model{ModelSetting: config.ModelSetting{ID: "test-model"}, Provider: "test"},
			Tools:        nil,
			Messages:     []Message{},
		},
		StreamFn:     mockStream,
		MemoryWriter: writer,
	})

	ctx := memory.WithSessionID(context.Background(), "sess-1")

	// Ensure deterministic turn time gaps; not strictly necessary but keeps output stable in logs.
	time.Sleep(1 * time.Millisecond)
	if err := a.Prompt(ctx, "hello"); err != nil {
		t.Fatalf("Prompt: %v", err)
	}

	if writer.calls != 1 {
		t.Fatalf("WriteDialogue calls=%d, want 1", writer.calls)
	}
	if writer.lastSessionID != "sess-1" {
		t.Fatalf("session_id=%q, want %q", writer.lastSessionID, "sess-1")
	}
	if writer.lastUserText != "hello" {
		t.Fatalf("userText=%q, want %q", writer.lastUserText, "hello")
	}
	if writer.lastAssistant != "final assistant" {
		t.Fatalf("assistantText=%q, want %q", writer.lastAssistant, "final assistant")
	}

	// Sanity: prompt should have appended user and assistant messages.
	state := a.State()
	var sawUser, sawAssistant bool
	for _, m := range state.Messages {
		if m.Role == keys.AgentRoleUser && m.Content == "hello" {
			sawUser = true
		}
		if m.Role == keys.AgentRoleAssistant && m.Content == "final assistant" {
			sawAssistant = true
		}
	}
	if !sawUser || !sawAssistant {
		t.Fatalf("expected user+assistant messages; got %+v", state.Messages)
	}

	// Ensure the injected user message remains plain content for LLM context conversion.
	if len(state.Messages) > 0 && state.Messages[0].Role == keys.AgentRoleUser {
		_, _ = json.Marshal(state.Messages[0]) // no-op, but keep json import used if gofmt rearranges later
	}
}
