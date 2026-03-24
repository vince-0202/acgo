package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/vince-0202/acgo/pkg/rag"
	"strings"

	"github.com/vince-0202/acgo/pkg/agent"
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

func (t *ragTool) Execute(ctx context.Context, toolCallID string, args json.RawMessage, update agent.ToolUpdateFunc) (agent.ToolResult, error) {
	var params struct {
		Query string  `json:"query"`
		TopK  float64 `json:"top_k"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return agent.ToolResult{}, fmt.Errorf("invalid arguments: %w", err)
	}
	if params.Query == "" {
		return agent.ToolResult{}, fmt.Errorf("query is required")
	}
	topK := int(params.TopK)
	if topK <= 0 {
		topK = 8
	}
	chunks, err := t.retriever.Retrieve(ctx, params.Query, topK, map[string]any{
		"mem_kind": "document",
	})
	if err != nil {
		return agent.ToolResult{Content: err.Error(), IsError: true}, nil
	}
	if len(chunks) == 0 {
		return agent.ToolResult{Content: "no relevant chunks found"}, nil
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
	return agent.ToolResult{Content: strings.TrimSpace(b.String())}, nil
}

// NewRagTool creates a new RAG search AgentTool.
func NewRagTool() agent.AgentTool {
	return &ragTool{
		retriever: rag.GetRetriever(),
	}
}
