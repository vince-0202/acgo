package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/vince-0202/acgo/pkg/agent"
	"github.com/vince-0202/acgo/pkg/communi"
	"github.com/vince-0202/acgo/pkg/keys"
	"github.com/vince-0202/acgo/pkg/memory"
)

type MemoryWriter interface {
	WriteDialogue(ctx context.Context, sessionID string, userText string, assistantText string) error
}

type MemoryWriterRuntimeAware interface {
	BindRuntime(agent.AgentRuntime)
}

type MemoryController struct {
	writers []MemoryWriter
	mu      sync.Mutex
	pending map[string]string
}

func NewMemoryController(writers ...MemoryWriter) *MemoryController {
	return &MemoryController{
		writers: append([]MemoryWriter(nil), writers...),
		pending: make(map[string]string),
	}
}

func (mc *MemoryController) Name() string {
	return "memory"
}

func (mc *MemoryController) Install(runtime agent.AgentRuntime) (func(), error) {
	mc.loadWriters()
	mc.bindRuntime(runtime)
	mc.installRecallPrompt(runtime)
	unsub := runtime.Subscribe(func(event agent.Event, abort func()) {
		switch event.Type {
		case agent.EventAgentStart:
			mc.installRecallPrompt(runtime)
			return
		case agent.EventMessageEnd:
			if event.Message == nil {
				return
			}
		default:
			return
		}
		turnID := strings.TrimSpace(event.TurnID)
		if turnID == "" {
			return
		}
		text := strings.TrimSpace(event.Message.ContentBlocksToText())
		switch event.Message.Role {
		case keys.AgentRoleUser:
			mc.rememberUser(turnID, text)
		case keys.AgentRoleAssistant:
			mc.writeDialogue(turnID, text)
		}
	})
	return func() {
		unsub()
	}, nil
}

func (mc *MemoryController) loadWriters() {
	if mc == nil || len(mc.writers) > 0 {
		return
	}
	writer, err := NewVectorDBMemoryWriter()
	if err != nil {
		return
	}
	mc.writers = []MemoryWriter{writer}
}

func (mc *MemoryController) bindRuntime(runtime agent.AgentRuntime) {
	if mc == nil || runtime == nil {
		return
	}
	for _, writer := range mc.writers {
		if aware, ok := writer.(MemoryWriterRuntimeAware); ok && aware != nil {
			aware.BindRuntime(runtime)
		}
	}
}

func (mc *MemoryController) installRecallPrompt(runtime agent.AgentRuntime) {
	if mc == nil || runtime == nil || runtime.ContextManager() == nil {
		return
	}
	runtime.ContextManager().UpsertPersistentPrompt("memory_recall", memoryRecallSystemPrompt)
}

func (mc *MemoryController) rememberUser(turnID, text string) {
	if mc == nil {
		return
	}
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.pending[turnID] = text
}

func (mc *MemoryController) writeDialogue(turnID, assistantText string) {
	if mc == nil {
		return
	}
	mc.mu.Lock()
	userText := mc.pending[turnID]
	delete(mc.pending, turnID)
	writers := append([]MemoryWriter(nil), mc.writers...)
	mc.mu.Unlock()

	if len(writers) == 0 {
		return
	}

	var wg sync.WaitGroup
	errCh := make(chan error, len(writers))
	for _, writer := range writers {
		if writer == nil {
			continue
		}
		wg.Add(1)
		go func(w MemoryWriter) {
			defer wg.Done()
			if err := w.WriteDialogue(context.Background(), "", userText, assistantText); err != nil {
				errCh <- fmt.Errorf("%T: %w", w, err)
			}
		}(writer)
	}
	wg.Wait()
	close(errCh)

	var joined error
	for err := range errCh {
		joined = errors.Join(joined, err)
	}
	_ = joined
}

type ContextMemoryWriter struct {
	mu      sync.RWMutex
	context agent.ContextRuntime
}

func NewContextMemoryWriter() *ContextMemoryWriter {
	return &ContextMemoryWriter{}
}

func (w *ContextMemoryWriter) BindRuntime(runtime agent.AgentRuntime) {
	if w == nil || runtime == nil {
		return
	}
	w.mu.Lock()
	w.context = runtime.ContextManager()
	w.mu.Unlock()
}

func (w *ContextMemoryWriter) WriteDialogue(_ context.Context, sessionID string, userText string, assistantText string) error {
	if w == nil {
		return nil
	}
	w.mu.RLock()
	ctxRuntime := w.context
	w.mu.RUnlock()
	if ctxRuntime == nil {
		return nil
	}
	text := formatMemoryEntry(userText, assistantText)
	if text == "" {
		return nil
	}
	msg := communi.NewSystemMessageWithoutId("[Memory]\n" + text)
	msg.AppendMetadata("memory_writer", "context")
	if sessionID != "" {
		msg.AppendMetadata("session_id", sessionID)
	}
	ctxRuntime.AppendMessage(msg)
	return nil
}

type FileMemoryWriter struct {
	path string
	mu   sync.Mutex
}

type fileMemoryRecord struct {
	Timestamp     time.Time `json:"timestamp"`
	SessionID     string    `json:"session_id,omitempty"`
	UserText      string    `json:"user_text,omitempty"`
	AssistantText string    `json:"assistant_text,omitempty"`
}

func NewFileMemoryWriter(path string) *FileMemoryWriter {
	return &FileMemoryWriter{path: strings.TrimSpace(path)}
}

func (w *FileMemoryWriter) WriteDialogue(_ context.Context, sessionID string, userText string, assistantText string) error {
	if w == nil || strings.TrimSpace(w.path) == "" {
		return nil
	}
	record := fileMemoryRecord{
		Timestamp:     time.Now().UTC(),
		SessionID:     strings.TrimSpace(sessionID),
		UserText:      strings.TrimSpace(userText),
		AssistantText: strings.TrimSpace(assistantText),
	}
	line, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(w.path), 0o755); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return err
	}
	return nil
}

type VectorDBMemoryWriter struct {
	manager *memory.Manager
}

func NewVectorDBMemoryWriter() (*VectorDBMemoryWriter, error) {
	mgr, err := memory.DefaultManager()
	if err != nil {
		return nil, err
	}
	return &VectorDBMemoryWriter{manager: mgr}, nil
}

func (w *VectorDBMemoryWriter) WriteDialogue(ctx context.Context, sessionID string, userText string, assistantText string) error {
	if w == nil || w.manager == nil {
		return nil
	}
	return w.manager.WriteDialogue(ctx, sessionID, userText, assistantText)
}

func formatMemoryEntry(userText, assistantText string) string {
	userText = strings.TrimSpace(userText)
	assistantText = strings.TrimSpace(assistantText)
	switch {
	case userText == "" && assistantText == "":
		return ""
	case userText == "":
		return "assistant: " + assistantText
	case assistantText == "":
		return "user: " + userText
	default:
		return "user: " + userText + "\nassistant: " + assistantText
	}
}

const memoryRecallSystemPrompt = `
Memory policy:
- Use the memory_recall tool when the user asks about prior preferences, earlier decisions, long-running tasks, or historical context that may no longer be in the current conversation window.
- Build concise search queries around stable entities such as names, goals, projects, constraints, and explicit decisions.
- Treat recalled memories as supporting context, not guaranteed truth. If a recalled item affects correctness, verify it against the current workspace or fresh tool results before relying on it.
- If memory_recall returns nothing useful, continue normally instead of inventing past memories.
`
