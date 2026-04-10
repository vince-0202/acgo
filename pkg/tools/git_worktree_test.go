package tools

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vince-0202/acgo/pkg/harness"
)

func TestGitWorktreeTool_List(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %s", err, out)
		}
	}
	run("git", "init")
	run("git", "config", "user.email", "t@t")
	run("git", "config", "user.name", "t")
	_ = exec.Command("git", "-C", dir, "commit", "--allow-empty", "-m", "init").Run()

	ctx := harness.ContextWithToolWorkingDir(context.Background(), dir)
	res := NewGitWorktreeTool().Execute(ctx, "c1", []byte(`{"action":"list","repo_path":"`+dir+`"}`), nil)
	if res.IsError() {
		t.Fatal(toolResultText(res))
	}
	txt := toolResultText(res)
	if !strings.Contains(txt, "worktree") && !strings.Contains(txt, dir) {
		t.Fatalf("unexpected list output: %q", txt)
	}
}

func TestAgentWorktreeDir(t *testing.T) {
	got := AgentWorktreeDir("/agent/base")
	want := filepath.Join("/agent/base", "worktrees")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
