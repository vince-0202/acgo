package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/vince-0202/acgo/pkg/communi"
	"github.com/vince-0202/acgo/pkg/harness"
	"github.com/vince-0202/acgo/pkg/memory"
	"strings"
)

// memoryRecallTool exposes long-term memory recall via vector search.
type memoryRecallTool struct {
	manager *memory.Manager
}

func (t *memoryRecallTool) Name() string  { return "memory_recall" }
func (t *memoryRecallTool) Label() string { return "Memory Recall" }
func (t *memoryRecallTool) Description() string {
	return "Recall relevant long-term memories from your dialogue history. Provide a query and optional memory_types/top_k."
}

func (t *memoryRecallTool) JSONSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"minLength":   1,
				"description": "Natural language query to search in long-term memories",
			},
			"memory_types": map[string]any{
				"type":        "array",
				"description": "Optional list of memory types to recall (default: [dialogue_raw])",
				"items": map[string]any{
					"type": "string",
				},
			},
			"top_k": map[string]any{
				"type":        "number",
				"description": "Maximum number of memories to retrieve (default: 8)",
			},
		},
		"required": []string{"query"},
	}
}

func (t *memoryRecallTool) Execute(ctx context.Context, toolCallID string, args json.RawMessage, update harness.ToolUpdateFunc) communi.ToolCallResult {
	var params struct {
		Query       string   `json:"query"`
		MemoryTypes []string `json:"memory_types"`
		TopK        float64  `json:"top_k"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return communi.ErrorToolCallResult(toolCallID, err)
	}
	topK := int(params.TopK)
	if topK <= 0 {
		topK = 8
	}

	if t == nil || t.manager == nil {
		return communi.NewToolCallResult(toolCallID, "memory store not configured")
	}

	var mts []memory.MemoryType
	if len(params.MemoryTypes) > 0 {
		mts = make([]memory.MemoryType, 0, len(params.MemoryTypes))
		for _, s := range params.MemoryTypes {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			mts = append(mts, memory.MemoryType(s))
		}
	}

	// Long-term recall does not require session_id isolation.
	chunks, err := t.manager.Recall(ctx, params.Query, "", mts, topK)
	if err != nil {
		return communi.ErrorToolCallResult(toolCallID, err)
	}
	if len(chunks) == 0 {
		return communi.NewToolCallResult(toolCallID, "no relevant memories found")
	}

	var b strings.Builder
	for i, c := range chunks {
		memType, _ := c.Metadata["memory_type"].(string)
		if memType == "" {
			if v, ok := c.Metadata["memory_type"]; ok && v != nil {
				memType = fmt.Sprint(v)
			}
		}
		fmt.Fprintf(&b, "[%d] score=%.3f type=%s", i+1, c.Score, memType)
		b.WriteString("\n")
		if c.Text != "" {
			b.WriteString(c.Text)
		}
		if !strings.HasSuffix(c.Text, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	return communi.NewToolCallResult(toolCallID, strings.TrimSpace(b.String()))
}

func NewMemoryRecallTool() harness.Tool {
	mgr, err := memory.DefaultManager()
	if err != nil {
		// Keep tool creatable even if memory is not configured; execute returns
		// a clear error instead of panicking.
		return &memoryRecallTool{manager: nil}
	}
	return &memoryRecallTool{manager: mgr}
}
