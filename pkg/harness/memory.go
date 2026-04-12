package harness

import (
	"context"
	"strings"
	"sync"

	"github.com/vince-0202/acgo/pkg/agent"
	"github.com/vince-0202/acgo/pkg/keys"
	"github.com/vince-0202/acgo/pkg/memory"
)

type MemoryWriter interface {
	WriteDialogue(ctx context.Context, sessionID string, userText string, assistantText string) error
}

type MemoryController struct {
	writer  MemoryWriter
	mu      sync.Mutex
	pending map[string]string
}

func NewMemoryController(w MemoryWriter) *MemoryController {
	return &MemoryController{
		writer:  w,
		pending: make(map[string]string),
	}
}

func (mc *MemoryController) Name() string {
	return "memory"
}

func (mc *MemoryController) Install(agent agent.AgentRuntime) (func(), error) {
	mc.loadWriter()
	unsub := agent.Subscribe(func(event agent.Event, abort func()) {
		if event.Type != agent.EventMessageEnd || event.Message == nil {
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

func (mc *MemoryController) loadWriter() {
	if mc == nil || mc.writer != nil {
		return
	}
	mgr, err := memory.DefaultManager()
	if err != nil {
		return
	}
	mc.writer = mgr
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
	defer mc.mu.Unlock()
	if mc.writer == nil {
		return
	}
	userText := mc.pending[turnID]
	delete(mc.pending, turnID)
	_ = mc.writer.WriteDialogue(context.Background(), "", userText, assistantText)
}
