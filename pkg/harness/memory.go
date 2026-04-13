package harness

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/vince-0202/acgo/pkg/agent"
	"github.com/vince-0202/acgo/pkg/keys"
	"github.com/vince-0202/acgo/pkg/memory"
)

type MemoryWriter = memory.MemoryWriter

type MemoryControllerOptions struct {
	Writers []MemoryWriter
	Caller  memory.MemoryCaller
}

type MemoryController struct {
	writers []MemoryWriter
	caller  memory.MemoryCaller
	mu      sync.Mutex
	pending map[string]string
}

func NewMemoryController(writers ...MemoryWriter) *MemoryController {
	return NewMemoryControllerWithOptions(MemoryControllerOptions{
		Writers: writers,
	})
}

func NewMemoryControllerWithOptions(opts MemoryControllerOptions) *MemoryController {
	return &MemoryController{
		writers: append([]MemoryWriter(nil), opts.Writers...),
		caller:  opts.Caller,
		pending: make(map[string]string),
	}
}

func (mc *MemoryController) Name() string {
	return "memory"
}

func (mc *MemoryController) Install(runtime agent.AgentRuntime) (func(), error) {
	mc.syncRecallPrompt(runtime)
	unsub := runtime.Subscribe(func(event agent.Event, abort func()) {
		mc.handleEvent(runtime, event)
	})
	return func() {
		unsub()
	}, nil
}

func (mc *MemoryController) handleEvent(runtime agent.AgentRuntime, event agent.Event) {
	if mc == nil {
		return
	}
	switch event.Type {
	case agent.EventAgentStart:
		mc.syncRecallPrompt(runtime)
	case agent.EventMessageEnd:
		mc.handleMessageEnd(event)
	}
}

func (mc *MemoryController) syncRecallPrompt(runtime agent.AgentRuntime) {
	if mc == nil || runtime == nil || runtime.ContextManager() == nil {
		return
	}
	if mc.caller == nil {
		runtime.ContextManager().RemovePersistentPrompt("memory_recall")
		return
	}
	runtime.ContextManager().UpsertPersistentPrompt("memory_recall", mc.recallPrompt())
}

func (mc *MemoryController) handleMessageEnd(event agent.Event) {
	if mc == nil || event.Message == nil {
		return
	}
	turnID := strings.TrimSpace(event.TurnID)
	if turnID == "" {
		return
	}
	text := strings.TrimSpace(event.Message.ContentBlocksToText())
	switch event.Message.Role {
	case keys.AgentRoleUser:
		mc.recordUserText(turnID, text)
	case keys.AgentRoleAssistant:
		mc.flushDialogue(turnID, text)
	}
}

func (mc *MemoryController) recordUserText(turnID, text string) {
	if mc == nil {
		return
	}
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.pending[turnID] = text
}

func (mc *MemoryController) flushDialogue(turnID, assistantText string) {
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

func (mc *MemoryController) recallPrompt() string {
	types := []string{"dialogue_raw"}
	if mc != nil && mc.caller != nil {
		defaultTypes := mc.caller.DefaultMemoryTypes()
		if len(defaultTypes) > 0 {
			types = types[:0]
			for _, mt := range defaultTypes {
				if mt == "" {
					continue
				}
				types = append(types, string(mt))
			}
		}
	}
	return `
Memory policy:
- Use the memory_recall tool when the user asks about prior preferences, earlier decisions, long-running tasks, or historical context that may no longer be in the current conversation window.
- Build concise search queries around stable entities such as names, goals, projects, constraints, and explicit decisions.
- Prefer these memory types when you do not need to specify one explicitly: ` + strings.Join(types, ", ") + `.
- Treat recalled memories as supporting context, not guaranteed truth. If a recalled item affects correctness, verify it against the current workspace or fresh tool results before relying on it.
- If memory_recall returns nothing useful, continue normally instead of inventing past memories.
`
}
