package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/vince-0202/acgo/pkg/agent"
	"github.com/vince-0202/acgo/pkg/communi"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	defaultGrepMaxResults  = 50
	defaultGrepMaxFiles    = 5000
	defaultGrepMaxFileSize = 2 * 1024 * 1024 // 2MB
)

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
				"minLength":   1,
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

func (t *grepTool) Execute(ctx context.Context, toolCallID string, args json.RawMessage, update agent.ToolUpdateFunc) communi.ToolCallResult {
	var params struct {
		Pattern    string  `json:"pattern"`
		Path       string  `json:"path"`
		MaxResults float64 `json:"max_results"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return communi.ErrorToolCallResult(toolCallID, err)
	}
	root := params.Path
	if root == "" {
		root = "."
	}
	root = agent.ResolveToolPath(ctx, root)
	maxResults := int(params.MaxResults)
	if maxResults <= 0 {
		maxResults = defaultGrepMaxResults
	}

	re, err := regexp.Compile(params.Pattern)
	if err != nil {
		return communi.ErrorToolCallResult(toolCallID, err)
	}

	var out strings.Builder
	n := 0
	scannedFiles := 0
	scanCapped := false
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
		if scannedFiles >= defaultGrepMaxFiles {
			scanCapped = true
			return filepath.SkipAll
		}
		if info.Size() > defaultGrepMaxFileSize {
			return nil
		}
		if n >= maxResults {
			return filepath.SkipAll
		}
		scannedFiles++
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
		return communi.ErrorToolCallResult(toolCallID, walkErr)
	}
	if out.Len() == 0 {
		return communi.NewToolCallResult(toolCallID, "no matches found")
	}
	if n >= maxResults {
		fmt.Fprintf(&out, "\n(truncated at %d results)", maxResults)
	}
	if scanCapped {
		fmt.Fprintf(&out, "\n(scan capped at %d files; narrow path or pattern for more complete results)", defaultGrepMaxFiles)
	}
	return communi.NewToolCallResult(toolCallID, strings.TrimSuffix(out.String(), "\n"))
}

// NewGrepTool creates a new grep AgentTool.
func NewGrepTool() agent.Tool {
	return &grepTool{}
}
