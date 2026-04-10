package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/vince-0202/acgo/pkg/tools"
)

func gitRevParseShowToplevel(dir string) (string, error) {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func sanitizeWorktreeSegment(name string) string {
	name = strings.TrimSpace(name)
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "wt"
	}
	return b.String()
}

func (m *Model) mainGitRepo() string {
	if rp := strings.TrimSpace(m.agent.ProjectRoot()); rp != "" {
		if root, err := gitRevParseShowToplevel(rp); err == nil {
			return root
		}
		return rp
	}
	if strings.TrimSpace(m.sessionProjectDir) != "" {
		if root, err := gitRevParseShowToplevel(m.sessionProjectDir); err == nil {
			return root
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		if root, err := gitRevParseShowToplevel(cwd); err == nil {
			return root
		}
	}
	return ""
}

func (m *Model) registerWorktreeCommands() {
	m.registerCommand(commandSpec{
		Name:    "worktree",
		Aliases: []string{"wt"},
		Usage:   "/worktree [status|set <path>|clear|add <name> [branch]|list|remove <name>]",
		Help:    "Use a git worktree or any directory as the agent workspace (tools cwd, skills, SYSTEM.md).",
		Handle: func(m *Model, arg string) (string, bool) {
			return m.handleWorktreeCommand(arg)
		},
	})
}

func (m *Model) handleWorktreeCommand(arg string) (string, bool) {
	arg = strings.TrimSpace(arg)
	fields := strings.Fields(arg)
	sub := "status"
	if len(fields) > 0 {
		sub = strings.ToLower(fields[0])
	}
	switch sub {
	case "status", "":
		return m.worktreeStatus()
	case "clear":
		if err := m.agent.SetProjectRoot(""); err != nil {
			return "clear failed: " + err.Error(), false
		}
		return "project root cleared (tools fall back to agent sandbox / process cwd)", false
	case "set":
		path := strings.TrimSpace(strings.TrimPrefix(arg, "set"))
		if path == "" {
			return "usage: /worktree set <path>", false
		}
		if err := m.agent.SetProjectRoot(path); err != nil {
			return "set failed: " + err.Error(), false
		}
		return "project root: " + m.agent.ProjectRoot(), false
	case "add":
		if len(fields) < 2 {
			return "usage: /worktree add <name> [branch]", false
		}
		name := fields[1]
		branch := ""
		if len(fields) > 2 {
			branch = strings.Join(fields[2:], " ")
		}
		main := m.mainGitRepo()
		if main == "" {
			return "no git repository detected (cd into a clone or /worktree set <repo> first)", false
		}
		seg := sanitizeWorktreeSegment(name)
		wtBase := tools.AgentWorktreeDir(m.agent.State().WorkDir)
		if err := os.MkdirAll(wtBase, 0o755); err != nil {
			return "mkdir worktrees: " + err.Error(), false
		}
		wtPath := filepath.Join(wtBase, seg)
		gitArgs := []string{"-C", main, "worktree", "add", wtPath}
		if branch != "" {
			gitArgs = append(gitArgs, branch)
		}
		out, err := exec.Command("git", gitArgs...).CombinedOutput()
		if err != nil {
			return fmt.Sprintf("git worktree add failed: %v\n%s", err, strings.TrimSpace(string(out))), false
		}
		if err := m.agent.SetProjectRoot(wtPath); err != nil {
			return "worktree created but set project root failed: " + err.Error(), false
		}
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return msg + "\nactive: " + wtPath, false
		}
		return "active: " + wtPath, false
	case "list":
		main := m.mainGitRepo()
		if main == "" {
			return "no git repository detected", false
		}
		out, err := exec.Command("git", "-C", main, "worktree", "list").CombinedOutput()
		if err != nil {
			return "git worktree list: " + err.Error(), false
		}
		return strings.TrimSpace(string(out)), false
	case "remove":
		if len(fields) < 2 {
			return "usage: /worktree remove <name>", false
		}
		name := fields[1]
		main := m.mainGitRepo()
		if main == "" {
			return "no git repository detected", false
		}
		seg := sanitizeWorktreeSegment(name)
		wtPath := filepath.Join(tools.AgentWorktreeDir(m.agent.State().WorkDir), seg)
		if strings.TrimSpace(m.agent.ProjectRoot()) == wtPath {
			if err := m.agent.SetProjectRoot(""); err != nil {
				return "clear active project before remove failed: " + err.Error(), false
			}
		}
		out, err := exec.Command("git", "-C", main, "worktree", "remove", wtPath).CombinedOutput()
		if err != nil {
			return fmt.Sprintf("git worktree remove failed: %v\n%s", err, strings.TrimSpace(string(out))), false
		}
		return "removed: " + wtPath, false
	default:
		return "unknown /worktree subcommand; try /help", false
	}
}

func (m *Model) worktreeStatus() (string, bool) {
	var b strings.Builder
	if pr := strings.TrimSpace(m.agent.ProjectRoot()); pr != "" {
		b.WriteString("active project root: " + pr + "\n")
	} else {
		b.WriteString("active project root: (not set)\n")
	}
	b.WriteString("session cwd: " + m.sessionProjectDir + "\n")
	main := m.mainGitRepo()
	if main != "" {
		b.WriteString("main git repo: " + main + "\n")
		out, err := exec.Command("git", "-C", main, "worktree", "list").Output()
		if err == nil {
			b.WriteString(strings.TrimSpace(string(out)))
		} else {
			b.WriteString("(worktree list failed: " + err.Error() + ")")
		}
	} else {
		b.WriteString("main git repo: (not detected)\n")
	}
	b.WriteString("\nagent worktree dir: " + tools.AgentWorktreeDir(m.agent.State().WorkDir))
	return strings.TrimSpace(b.String()), false
}
