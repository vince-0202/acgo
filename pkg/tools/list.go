package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/vince-0202/acgo/pkg/communi"
	"github.com/vince-0202/acgo/pkg/harness"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type listTool struct{}

func (t *listTool) Name() string  { return "list" }
func (t *listTool) Label() string { return "List Directory" }
func (t *listTool) Description() string {
	return "List files and directories under a path. Use to explore project structure. Optional glob filter (e.g. '*.go') to match names."
}

func (t *listTool) JSONSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Directory path to list (default: current directory .)",
			},
			"glob": map[string]any{
				"type":        "string",
				"description": "Optional glob pattern to filter entries (e.g. '*.go', '*.md')",
			},
		},
		"required": []string{},
	}
}

func (t *listTool) Execute(ctx context.Context, toolCallID string, args json.RawMessage, update harness.ToolUpdateFunc) communi.ToolCallResult {
	var params struct {
		Path string `json:"path"`
		Glob string `json:"glob"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return communi.ErrorToolCallResult(toolCallID, err)
	}
	dir := params.Path
	if dir == "" {
		dir = "."
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return communi.ErrorToolCallResult(toolCallID, err)
	}
	var names []string
	for _, e := range entries {
		name := e.Name()
		if params.Glob != "" {
			ok, _ := filepath.Match(params.Glob, name)
			if !ok {
				continue
			}
		}
		if e.IsDir() {
			name += "/"
		}
		names = append(names, name)
	}
	sort.Strings(names)
	content := strings.Join(names, "\n")
	if content == "" {
		content = "(no matching entries)"
	} else {
		content = fmt.Sprintf("%s\n--- %d entries", content, len(names))
	}
	return communi.NewToolCallResult(toolCallID, content)
}

// NewListTool creates a new list (ls/find) AgentTool.
func NewListTool() harness.Tool {
	return &listTool{}
}
