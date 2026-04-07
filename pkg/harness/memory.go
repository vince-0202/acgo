package harness

import (
	"context"
	"strings"
	"sync"

	"github.com/vince-0202/acgo/pkg/communi"
	"github.com/vince-0202/acgo/pkg/memory"
)

// MemoryWriter persists user–assistant dialogue for long-term recall.
type MemoryWriter interface {
	WriteDialogue(ctx context.Context, sessionID string, userText string, assistantText string) error
}

// MemoryController pairs per-turn user/assistant messages and writes them via MemoryWriter.
// Load resolves a default writer when none was provided (see memory.DefaultManager).
type MemoryController struct {
	writer  MemoryWriter
	mu      sync.Mutex
	pending map[string]string // turnID -> user text
}

func NewMemoryController(w MemoryWriter) *MemoryController {
	return &MemoryController{
		writer:  w,
		pending: make(map[string]string),
	}
}

// Load implements Controller. When no writer was injected, tries memory.DefaultManager().
func (c *MemoryController) Load() {
	if c == nil {
		return
	}
	if c.writer != nil {
		return
	}
	mgr, err := memory.DefaultManager()
	if err != nil {
		return
	}
	c.writer = mgr
}

func (c *MemoryController) RecordWithMetaData(ctx context.Context, msg *communi.Message, metaData map[string]any) {
	if c == nil || msg == nil || metaData == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.writer == nil {
		return
	}
	turnID, _ := metaData["turnID"].(string)
	if turnID == "" {
		return
	}
	typ, _ := metaData["type"].(string)
	text := strings.TrimSpace(msg.ContentBlocksToText())
	switch typ {
	case "userAsk":
		c.pending[turnID] = text
	case "assistant":
		userText := c.pending[turnID]
		delete(c.pending, turnID)
		sid, _ := memory.SessionIDFromContext(ctx)
		_ = c.writer.WriteDialogue(ctx, sid, userText, text)
	}
}
