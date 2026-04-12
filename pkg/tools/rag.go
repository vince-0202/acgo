package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/vince-0202/acgo/pkg/agent"
	"github.com/vince-0202/acgo/pkg/communi"
	"github.com/vince-0202/acgo/pkg/rag"
	"strings"
)

// ragTool exposes semantic search over the indexed document chunks.
type ragTool struct {
	retriever rag.Retriever
}

func (t *ragTool) Name() string  { return "rag_search" }
func (t *ragTool) Label() string { return "RAG Search" }
func (t *ragTool) Description() string {
	return "Semantic search over indexed local documents. Provide a natural language query and optional top_k to retrieve relevant chunks."
}

func (t *ragTool) JSONSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"minLength":   1,
				"description": "Natural language query to search in the document index",
			},
			"top_k": map[string]any{
				"type":        "number",
				"description": "Maximum number of chunks to retrieve (default: 8)",
			},
		},
		"required": []string{"query"},
	}
}

func (t *ragTool) Execute(ctx context.Context, toolCallID string, args json.RawMessage, update agent.ToolUpdateFunc) communi.ToolCallResult {
	var params struct {
		Query string  `json:"query"`
		TopK  float64 `json:"top_k"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return communi.ErrorToolCallResult(toolCallID, err)
	}
	topK := int(params.TopK)
	if topK <= 0 {
		topK = 8
	}
	chunks, err := t.retriever.Retrieve(ctx, params.Query, topK, map[string]any{
		"mem_kind": "document",
	})
	if err != nil {
		return communi.ErrorToolCallResult(toolCallID, err)
	}
	if len(chunks) == 0 {
		return communi.NewToolCallResult(toolCallID, "no relevant chunks found")
	}
	var b strings.Builder
	for i, c := range chunks {
		fmt.Fprintf(&b, "[%d] score=%.3f\n", i+1, c.Score)
		if path, ok := c.Metadata["file_path"].(string); ok && path != "" {
			fmt.Fprintf(&b, "file: %s\n", path)
		}
		if idx, ok := c.Metadata["chunk_index"]; ok {
			fmt.Fprintf(&b, "chunk_index: %v\n", idx)
		}
		b.WriteString(c.Text)
		if !strings.HasSuffix(c.Text, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	return communi.NewToolCallResult(toolCallID, strings.TrimSpace(b.String()))
}

// NewRagTool creates a new RAG search AgentTool.
func NewRagTool() agent.Tool {
	return &ragTool{
		retriever: rag.GetRetriever(),
	}
}
