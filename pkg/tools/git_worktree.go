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

type gitWorktreeTool struct{}

func (t *gitWorktreeTool) Name() string  { return "git_worktree" }
func (t *gitWorktreeTool) Label() string { return "Git Worktree" }
func (t *gitWorktreeTool) Description() string {
	return "List, add, or remove git worktrees. repo_path defaults to the agent project directory or current tool cwd. Use for isolated checkouts without switching the main clone."
}

func (t *gitWorktreeTool) JSONSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"list", "add", "remove"},
				"description": "list: git worktree list; add: create path; remove: delete worktree",
			},
			"repo_path": map[string]any{
				"type":        "string",
				"description": "Main repository path (default: agent project root / .)",
			},
			"worktree_path": map[string]any{
				"type":        "string",
				"description": "Absolute or relative path for new/existing worktree (required for add/remove)",
			},
			"branch": map[string]any{
				"type":        "string",
				"description": "Optional branch/commit for add (second argument to git worktree add)",
			},
			"force": map[string]any{
				"type":        "boolean",
				"description": "For remove: pass --force when the worktree has uncommitted changes or is locked",
			},
		},
		"required": []string{"action"},
	}
}

func (t *gitWorktreeTool) Execute(ctx context.Context, toolCallID string, args json.RawMessage, update harness.ToolUpdateFunc) communi.ToolCallResult {
	var params struct {
		Action       string `json:"action"`
		RepoPath     string `json:"repo_path"`
		WorktreePath string `json:"worktree_path"`
		Branch       string `json:"branch"`
		Force        bool   `json:"force"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return communi.ErrorToolCallResult(toolCallID, err)
	}
	action := strings.ToLower(strings.TrimSpace(params.Action))
	repo := strings.TrimSpace(params.RepoPath)
	if repo == "" {
		repo = "."
	}
	repo = harness.ResolveToolPath(ctx, repo)

	switch action {
	case "list":
		out, err := exec.CommandContext(ctx, "git", "-C", repo, "worktree", "list", "--porcelain").CombinedOutput()
		if err != nil {
			return communi.ErrorToolCallResult(toolCallID, fmt.Errorf("%w: %s", err, string(out)))
		}
		return communi.NewToolCallResult(toolCallID, strings.TrimSpace(string(out)))
	case "add":
		wt := strings.TrimSpace(params.WorktreePath)
		if wt == "" {
			return communi.ErrorToolCallResult(toolCallID, fmt.Errorf("worktree_path is required for add"))
		}
		wt = harness.ResolveToolPath(ctx, wt)
		gitArgs := []string{"-C", repo, "worktree", "add", wt}
		br := strings.TrimSpace(params.Branch)
		if br != "" {
			gitArgs = append(gitArgs, br)
		}
		out, err := exec.CommandContext(ctx, "git", gitArgs...).CombinedOutput()
		if err != nil {
			return communi.ErrorToolCallResult(toolCallID, fmt.Errorf("%w: %s", err, string(out)))
		}
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = "worktree added: " + wt
		}
		return communi.NewToolCallResult(toolCallID, msg)
	case "remove":
		wt := strings.TrimSpace(params.WorktreePath)
		if wt == "" {
			return communi.ErrorToolCallResult(toolCallID, fmt.Errorf("worktree_path is required for remove"))
		}
		wt = harness.ResolveToolPath(ctx, wt)
		gitArgs := []string{"-C", repo, "worktree", "remove"}
		if params.Force {
			gitArgs = append(gitArgs, "--force")
		}
		gitArgs = append(gitArgs, wt)
		out, err := exec.CommandContext(ctx, "git", gitArgs...).CombinedOutput()
		if err != nil {
			return communi.ErrorToolCallResult(toolCallID, fmt.Errorf("%w: %s", err, string(out)))
		}
		return communi.NewToolCallResult(toolCallID, strings.TrimSpace(string(out)))
	default:
		return communi.ErrorToolCallResult(toolCallID, fmt.Errorf("unknown action %q (use list, add, remove)", action))
	}
}

// NewGitWorktreeTool registers git worktree operations for the agent.
func NewGitWorktreeTool() harness.Tool {
	return &gitWorktreeTool{}
}

// AgentWorktreeDir returns the default directory for agent-local worktrees under the agent sandbox.
func AgentWorktreeDir(agentStateWorkDir string) string {
	return filepath.Join(agentStateWorkDir, "worktrees")
}
