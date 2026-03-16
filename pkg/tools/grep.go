package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"acgo/pkg/agent"
)

const defaultGrepMaxResults = 50

type grepTool struct{}

func (t *grepTool) Name() string  { return "grep" }
func (t *grepTool) Label() string { return "Grep" }
func (t *grepTool) Description() string {
	return "Search for a regex pattern in files under a directory. Returns file path, line number, and matching line. Use for finding usages or definitions."
}

func (t *grepTool) JSONSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern": map[string]any{
				"type":        "string",
				"description": "Regex pattern to search for (e.g. 'func main', 'import')",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Directory to search in (default: current directory .)",
			},
			"max_results": map[string]any{
				"type":        "number",
				"description": "Maximum number of matches to return (default: 50)",
			},
		},
		"required": []string{"pattern"},
	}
}

func (t *grepTool) Execute(ctx context.Context, toolCallID string, args json.RawMessage, update agent.ToolUpdateFunc) (agent.ToolResult, error) {
	var params struct {
		Pattern    string  `json:"pattern"`
		Path       string  `json:"path"`
		MaxResults float64 `json:"max_results"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return agent.ToolResult{}, fmt.Errorf("invalid arguments: %w", err)
	}
	if params.Pattern == "" {
		return agent.ToolResult{}, fmt.Errorf("pattern is required")
	}
	root := params.Path
	if root == "" {
		root = "."
	}
	maxResults := int(params.MaxResults)
	if maxResults <= 0 {
		maxResults = defaultGrepMaxResults
	}

	re, err := regexp.Compile(params.Pattern)
	if err != nil {
		return agent.ToolResult{}, fmt.Errorf("invalid regex pattern: %w", err)
	}

	var out strings.Builder
	n := 0
	walkErr := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err != nil {
			if os.IsPermission(err) {
				return nil
			}
			return err
		}
		if info.IsDir() {
			if path != root && (strings.HasPrefix(filepath.Base(path), ".") || filepath.Base(path) == "node_modules" || filepath.Base(path) == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if n >= maxResults {
			return filepath.SkipAll
		}
		f, openErr := os.Open(path)
		if openErr != nil {
			return nil
		}
		sc := bufio.NewScanner(f)
		lineNum := 0
		for sc.Scan() {
			lineNum++
			if n >= maxResults {
				break
			}
			line := sc.Text()
			if re.MatchString(line) {
				n++
				fmt.Fprintf(&out, "%s:%d: %s\n", path, lineNum, line)
			}
		}
		_ = f.Close()
		return nil
	})
	if walkErr != nil {
		return agent.ToolResult{Content: walkErr.Error(), IsError: true}, nil
	}
	if out.Len() == 0 {
		return agent.ToolResult{Content: "no matches found"}, nil
	}
	if n >= maxResults {
		fmt.Fprintf(&out, "\n(truncated at %d results)", maxResults)
	}
	return agent.ToolResult{Content: strings.TrimSuffix(out.String(), "\n")}, nil
}

// NewGrepTool creates a new grep AgentTool.
func NewGrepTool() agent.AgentTool {
	return &grepTool{}
}
