package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/vince-0202/acgo/pkg/communi"
	"github.com/vince-0202/acgo/pkg/harness"
)

const (
	gitMaxOutputBytes = 512 * 1024
)

func resolveGitRepoRoot(ctx context.Context, dir string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		dir = "."
	}
	dir = harness.ResolveToolPath(ctx, dir)
	dir = filepath.Clean(dir)
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("not a git repository (or git failed): %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func runGitInRepo(ctx context.Context, repoRoot string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repoRoot}, args...)...)
	return cmd.CombinedOutput()
}

func truncateGitOutput(b []byte) (string, bool) {
	s := string(b)
	if len(b) <= gitMaxOutputBytes {
		return s, false
	}
	cut := string(b[:gitMaxOutputBytes])
	return cut + fmt.Sprintf("\n\n[output truncated at %d bytes]", gitMaxOutputBytes), true
}

// --- git_status ---

type gitStatusTool struct{}

func (t *gitStatusTool) Name() string  { return "git_status" }
func (t *gitStatusTool) Label() string { return "Git Status" }
func (t *gitStatusTool) Description() string {
	return "Show working tree status: branch, ahead/behind, staged/unstaged/untracked files (porcelain + short branch line). Use before committing or to see what changed."
}

func (t *gitStatusTool) JSONSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path inside the repository (default: current directory .)",
			},
		},
		"required": []string{},
	}
}

func (t *gitStatusTool) Execute(ctx context.Context, toolCallID string, args json.RawMessage, update harness.ToolUpdateFunc) communi.ToolCallResult {
	var params struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal(args, &params)
	repo, err := resolveGitRepoRoot(ctx, params.Path)
	if err != nil {
		return communi.ErrorToolCallResult(toolCallID, err)
	}
	out, err := runGitInRepo(ctx, repo, "status", "--porcelain=v2", "-b", "--untracked-files=normal")
	if err != nil {
		return communi.ErrorToolCallResult(toolCallID, fmt.Errorf("%w: %s", err, string(out)))
	}
	text, truncated := truncateGitOutput(out)
	if truncated {
		text += "\n(narrow path or use git_diff on specific files)"
	}
	return communi.NewToolCallResult(toolCallID, strings.TrimSpace(text))
}

func NewGitStatusTool() harness.Tool {
	return &gitStatusTool{}
}

// --- git_diff ---

type gitDiffTool struct{}

func (t *gitDiffTool) Name() string  { return "git_diff" }
func (t *gitDiffTool) Label() string { return "Git Diff" }
func (t *gitDiffTool) Description() string {
	return "Show a diff: unstaged changes (default), staged (--cached), or compare two revisions (base..head). Use to review edits before commit."
}

func (t *gitDiffTool) JSONSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path inside the repository (default: .)",
			},
			"scope": map[string]any{
				"type":        "string",
				"enum":        []string{"unstaged", "staged", "range"},
				"description": "unstaged: working tree vs index; staged: index vs HEAD; range: git diff base..head (requires base and head)",
			},
			"base": map[string]any{
				"type":        "string",
				"description": "For scope=range, start revision (e.g. main, HEAD~1)",
			},
			"head": map[string]any{
				"type":        "string",
				"description": "For scope=range, end revision (e.g. HEAD, topic-branch)",
			},
			"paths": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Optional file paths to limit the diff (repo-relative)",
			},
		},
		"required": []string{},
	}
}

func (t *gitDiffTool) Execute(ctx context.Context, toolCallID string, args json.RawMessage, update harness.ToolUpdateFunc) communi.ToolCallResult {
	var params struct {
		Path  string   `json:"path"`
		Scope string   `json:"scope"`
		Base  string   `json:"base"`
		Head  string   `json:"head"`
		Paths []string `json:"paths"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return communi.ErrorToolCallResult(toolCallID, err)
	}
	repo, err := resolveGitRepoRoot(ctx, params.Path)
	if err != nil {
		return communi.ErrorToolCallResult(toolCallID, err)
	}
	scope := strings.TrimSpace(params.Scope)
	if scope == "" {
		scope = "unstaged"
	}

	var gitArgs []string
	switch scope {
	case "unstaged":
		gitArgs = []string{"diff", "--no-color"}
	case "staged":
		gitArgs = []string{"diff", "--no-color", "--cached"}
	case "range":
		b, h := strings.TrimSpace(params.Base), strings.TrimSpace(params.Head)
		if b == "" || h == "" {
			return communi.ErrorToolCallResult(toolCallID, fmt.Errorf("scope range requires non-empty base and head"))
		}
		gitArgs = []string{"diff", "--no-color", b + ".." + h}
	default:
		return communi.ErrorToolCallResult(toolCallID, fmt.Errorf("invalid scope %q (use unstaged, staged, range)", scope))
	}
	var pathArgs []string
	for _, p := range params.Paths {
		p = strings.TrimSpace(p)
		if p != "" {
			pathArgs = append(pathArgs, p)
		}
	}
	if len(pathArgs) > 0 {
		gitArgs = append(gitArgs, "--")
		gitArgs = append(gitArgs, pathArgs...)
	}

	out, err := runGitInRepo(ctx, repo, gitArgs...)
	if err != nil {
		return communi.ErrorToolCallResult(toolCallID, fmt.Errorf("%w: %s", err, string(out)))
	}
	text, truncated := truncateGitOutput(out)
	if strings.TrimSpace(text) == "" {
		return communi.NewToolCallResult(toolCallID, "(no diff output)")
	}
	if truncated {
		text += "\n(use paths to narrow, or smaller revision range)"
	}
	return communi.NewToolCallResult(toolCallID, strings.TrimSpace(text))
}

func NewGitDiffTool() harness.Tool {
	return &gitDiffTool{}
}

// --- git_log ---

type gitLogTool struct{}

func (t *gitLogTool) Name() string  { return "git_log" }
func (t *gitLogTool) Label() string { return "Git Log" }
func (t *gitLogTool) Description() string {
	return "Show recent commits (oneline). Optional path filter limits history to those files."
}

func (t *gitLogTool) JSONSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path inside the repository (default: .)",
			},
			"limit": map[string]any{
				"type":        "number",
				"description": "Max commits (default 20, max 100)",
			},
			"paths": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Optional paths; if set, only commits touching these files",
			},
		},
		"required": []string{},
	}
}

func (t *gitLogTool) Execute(ctx context.Context, toolCallID string, args json.RawMessage, update harness.ToolUpdateFunc) communi.ToolCallResult {
	var params struct {
		Path  string   `json:"path"`
		Limit float64  `json:"limit"`
		Paths []string `json:"paths"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return communi.ErrorToolCallResult(toolCallID, err)
	}
	repo, err := resolveGitRepoRoot(ctx, params.Path)
	if err != nil {
		return communi.ErrorToolCallResult(toolCallID, err)
	}
	n := int(params.Limit)
	if n <= 0 {
		n = 20
	}
	if n > 100 {
		n = 100
	}
	gitArgs := []string{"log", "-n", fmt.Sprintf("%d", n), "--date=short", "--pretty=format:%h %ad %s"}
	gitArgs = append(gitArgs, "--")
	hasPath := false
	for _, p := range params.Paths {
		p = strings.TrimSpace(p)
		if p != "" {
			gitArgs = append(gitArgs, p)
			hasPath = true
		}
	}
	if !hasPath {
		gitArgs = gitArgs[:len(gitArgs)-1] // drop trailing "--"
	}

	out, err := runGitInRepo(ctx, repo, gitArgs...)
	if err != nil {
		return communi.ErrorToolCallResult(toolCallID, fmt.Errorf("%w: %s", err, string(out)))
	}
	text, truncated := truncateGitOutput(out)
	if strings.TrimSpace(text) == "" {
		return communi.NewToolCallResult(toolCallID, "(no commits)")
	}
	if truncated {
		text += "\n(reduce limit or narrow paths)"
	}
	return communi.NewToolCallResult(toolCallID, strings.TrimSpace(text))
}

func NewGitLogTool() harness.Tool {
	return &gitLogTool{}
}

// --- git_branch ---

type gitBranchTool struct{}

func (t *gitBranchTool) Name() string  { return "git_branch" }
func (t *gitBranchTool) Label() string { return "Git Branch" }
func (t *gitBranchTool) Description() string {
	return "List local branches (default) or all branches including remotes. Shows current branch."
}

func (t *gitBranchTool) JSONSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path inside the repository (default: .)",
			},
			"all": map[string]any{
				"type":        "boolean",
				"description": "If true, include remote-tracking branches (-a)",
			},
		},
		"required": []string{},
	}
}

func (t *gitBranchTool) Execute(ctx context.Context, toolCallID string, args json.RawMessage, update harness.ToolUpdateFunc) communi.ToolCallResult {
	var params struct {
		Path string `json:"path"`
		All  bool   `json:"all"`
	}
	_ = json.Unmarshal(args, &params)
	repo, err := resolveGitRepoRoot(ctx, params.Path)
	if err != nil {
		return communi.ErrorToolCallResult(toolCallID, err)
	}
	gitArgs := []string{"branch", "--verbose", "--no-color"}
	if params.All {
		gitArgs = append(gitArgs, "-a")
	}
	out, err := runGitInRepo(ctx, repo, gitArgs...)
	if err != nil {
		return communi.ErrorToolCallResult(toolCallID, fmt.Errorf("%w: %s", err, string(out)))
	}
	text, truncated := truncateGitOutput(out)
	if truncated {
		text += "\n(use all=false if too large)"
	}
	return communi.NewToolCallResult(toolCallID, strings.TrimSpace(text))
}

func NewGitBranchTool() harness.Tool {
	return &gitBranchTool{}
}

// --- git_add ---

type gitAddTool struct{}

func (t *gitAddTool) Name() string  { return "git_add" }
func (t *gitAddTool) Label() string { return "Git Add" }
func (t *gitAddTool) Description() string {
	return "Stage files for commit (git add). Pass explicit paths; does not use git add -A unless you pass \".\". Requires write access to the repo."
}

func (t *gitAddTool) JSONSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path inside the repository (default: .)",
			},
			"paths": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"minItems":    1,
				"description": "Paths to stage, relative to repo root or cwd (e.g. [\"pkg/foo.go\"])",
			},
		},
		"required": []string{"paths"},
	}
}

func (t *gitAddTool) Execute(ctx context.Context, toolCallID string, args json.RawMessage, update harness.ToolUpdateFunc) communi.ToolCallResult {
	var params struct {
		Path  string   `json:"path"`
		Paths []string `json:"paths"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return communi.ErrorToolCallResult(toolCallID, err)
	}
	if len(params.Paths) == 0 {
		return communi.ErrorToolCallResult(toolCallID, fmt.Errorf("paths must be non-empty"))
	}
	repo, err := resolveGitRepoRoot(ctx, params.Path)
	if err != nil {
		return communi.ErrorToolCallResult(toolCallID, err)
	}
	gitArgs := []string{"add", "--"}
	for _, p := range params.Paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		gitArgs = append(gitArgs, p)
	}
	if len(gitArgs) <= 2 {
		return communi.ErrorToolCallResult(toolCallID, fmt.Errorf("no valid paths to stage"))
	}
	out, err := runGitInRepo(ctx, repo, gitArgs...)
	if err != nil {
		return communi.ErrorToolCallResult(toolCallID, fmt.Errorf("%w: %s", err, string(out)))
	}
	msg := strings.TrimSpace(string(out))
	if msg == "" {
		msg = "ok"
	}
	return communi.NewToolCallResult(toolCallID, msg)
}

func NewGitAddTool() harness.Tool {
	return &gitAddTool{}
}

// --- git_commit ---

type gitCommitTool struct{}

func (t *gitCommitTool) Name() string  { return "git_commit" }
func (t *gitCommitTool) Label() string { return "Git Commit" }
func (t *gitCommitTool) Description() string {
	return "Create a commit from staged changes only (git commit -m). Stage files first with git_add."
}

func (t *gitCommitTool) JSONSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path inside the repository (default: .)",
			},
			"message": map[string]any{
				"type":        "string",
				"minLength":   1,
				"description": "Commit message",
			},
		},
		"required": []string{"message"},
	}
}

func (t *gitCommitTool) Execute(ctx context.Context, toolCallID string, args json.RawMessage, update harness.ToolUpdateFunc) communi.ToolCallResult {
	var params struct {
		Path    string `json:"path"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return communi.ErrorToolCallResult(toolCallID, err)
	}
	params.Message = strings.TrimSpace(params.Message)
	if params.Message == "" {
		return communi.ErrorToolCallResult(toolCallID, fmt.Errorf("message is required"))
	}
	repo, err := resolveGitRepoRoot(ctx, params.Path)
	if err != nil {
		return communi.ErrorToolCallResult(toolCallID, err)
	}
	out, err := runGitInRepo(ctx, repo, "commit", "-m", params.Message)
	if err != nil {
		return communi.ErrorToolCallResult(toolCallID, fmt.Errorf("%w: %s", err, string(out)))
	}
	text := strings.TrimSpace(string(out))
	if text == "" {
		text = "commit completed"
	}
	return communi.NewToolCallResult(toolCallID, text)
}

func NewGitCommitTool() harness.Tool {
	return &gitCommitTool{}
}
