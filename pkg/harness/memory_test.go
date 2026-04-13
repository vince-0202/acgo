package harness

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vince-0202/acgo/pkg/agent"
	"github.com/vince-0202/acgo/pkg/memory"
)

type sleepMemoryWriter struct {
	delay time.Duration
	count atomic.Int32
}

func (w *sleepMemoryWriter) WriteDialogue(_ context.Context, _ string, _, _ string) error {
	w.count.Add(1)
	time.Sleep(w.delay)
	return nil
}

func TestMemoryControllerWriteDialogueFansOutInParallel(t *testing.T) {
	w1 := &sleepMemoryWriter{delay: 120 * time.Millisecond}
	w2 := &sleepMemoryWriter{delay: 120 * time.Millisecond}
	mc := NewMemoryController(w1, w2)
	mc.recordUserText("turn-1", "hello")

	start := time.Now()
	mc.flushDialogue("turn-1", "world")
	elapsed := time.Since(start)

	if w1.count.Load() != 1 || w2.count.Load() != 1 {
		t.Fatalf("expected both writers to be called once, got %d and %d", w1.count.Load(), w2.count.Load())
	}
	if elapsed >= 220*time.Millisecond {
		t.Fatalf("expected parallel writes, elapsed=%v", elapsed)
	}
}

func TestFileMemoryWriterAppendsJSONL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory", "mem.jsonl")
	writer := memory.NewFileMemoryWriter(path)

	if err := writer.WriteDialogue(context.Background(), "sess-1", "u", "a"); err != nil {
		t.Fatalf("WriteDialogue error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}

	var record struct {
		SessionID     string `json:"session_id"`
		UserText      string `json:"user_text"`
		AssistantText string `json:"assistant_text"`
	}
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if record.SessionID != "sess-1" || record.UserText != "u" || record.AssistantText != "a" {
		t.Fatalf("unexpected record: %+v", record)
	}
}

func TestMemoryControllerDefaultDoesNotInstallRecallPrompt(t *testing.T) {
	ag := agent.New(agent.Options{ID: "ag-1", WorkDir: t.TempDir()})
	h := NewHarness(NewContextController(), NewMemoryController())
	if err := h.Attach(ag); err != nil {
		t.Fatalf("Attach error: %v", err)
	}
	defer h.Detach()

	ag.Emit(agent.NewEvent(
		agent.WithEventType(agent.EventAgentStart),
		agent.WithEventAgent(ag),
	))

	prompt := ag.ContextManager().SystemPrompt()
	if strings.Contains(prompt, "memory_recall") {
		t.Fatalf("did not expect memory_recall instructions by default, got %q", prompt)
	}
}

func TestMemoryControllerUsesCallerDefaultTypesInPrompt(t *testing.T) {
	ag := agent.New(agent.Options{ID: "ag-1", WorkDir: t.TempDir()})
	caller := memory.NewFileMemoryCallerWithTypes(filepath.Join(t.TempDir(), "memory.jsonl"), []memory.MemoryType{memory.DialogueRaw})
	h := NewHarness(
		NewContextController(),
		NewMemoryControllerWithOptions(MemoryControllerOptions{
			Writers: []MemoryWriter{},
			Caller:  caller,
		}),
	)
	if err := h.Attach(ag); err != nil {
		t.Fatalf("Attach error: %v", err)
	}
	defer h.Detach()

	ag.Emit(agent.NewEvent(
		agent.WithEventType(agent.EventAgentStart),
		agent.WithEventAgent(ag),
	))

	prompt := ag.ContextManager().SystemPrompt()
	if !strings.Contains(prompt, "memory_recall") || !strings.Contains(prompt, "dialogue_raw") {
		t.Fatalf("expected default memory type in prompt, got %q", prompt)
	}
}
