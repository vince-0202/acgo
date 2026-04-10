package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func initTestGitRepo(t *testing.T) string {
	t.Helper()
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
	run("git", "config", "user.email", "test@example.com")
	run("git", "config", "user.name", "test")
	f := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(f, []byte("v1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	run("git", "add", "hello.txt")
	run("git", "commit", "-m", "init")
	if err := os.WriteFile(f, []byte("v2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestGitStatusTool(t *testing.T) {
	dir := initTestGitRepo(t)
	ctx := context.Background()
	res := NewGitStatusTool().Execute(ctx, "c1", []byte(`{"path":"`+dir+`"}`), nil)
	if res.IsError() {
		t.Fatalf("status: %s", toolResultText(res))
	}
	if toolResultText(res) == "" {
		t.Fatal("empty status")
	}
}

func TestGitDiffTool_Unstaged(t *testing.T) {
	dir := initTestGitRepo(t)
	ctx := context.Background()
	res := NewGitDiffTool().Execute(ctx, "c1", []byte(`{"path":"`+dir+`","scope":"unstaged"}`), nil)
	if res.IsError() {
		t.Fatalf("diff: %s", toolResultText(res))
	}
	txt := toolResultText(res)
	if !strings.Contains(txt, "v1") && !strings.Contains(txt, "v2") {
		t.Errorf("expected diff body, got %q", txt)
	}
}

func TestGitLogTool(t *testing.T) {
	dir := initTestGitRepo(t)
	ctx := context.Background()
	res := NewGitLogTool().Execute(ctx, "c1", []byte(`{"path":"`+dir+`","limit":5}`), nil)
	if res.IsError() {
		t.Fatalf("log: %s", toolResultText(res))
	}
	if !strings.Contains(toolResultText(res), "init") {
		t.Errorf("expected commit message in log: %q", toolResultText(res))
	}
}

func TestGitBranchTool(t *testing.T) {
	dir := initTestGitRepo(t)
	ctx := context.Background()
	res := NewGitBranchTool().Execute(ctx, "c1", []byte(`{"path":"`+dir+`"}`), nil)
	if res.IsError() {
		t.Fatalf("branch: %s", toolResultText(res))
	}
	br := toolResultText(res)
	if !strings.Contains(br, "main") && !strings.Contains(br, "master") {
		t.Logf("branch output (default branch name varies): %q", br)
	}
}

func TestGitAddCommitTool(t *testing.T) {
	dir := initTestGitRepo(t)
	ctx := context.Background()
	addRes := NewGitAddTool().Execute(ctx, "c1", []byte(`{"path":"`+dir+`","paths":["hello.txt"]}`), nil)
	if addRes.IsError() {
		t.Fatalf("add: %s", toolResultText(addRes))
	}
	commitRes := NewGitCommitTool().Execute(ctx, "c2", []byte(`{"path":"`+dir+`","message":"second"}`), nil)
	if commitRes.IsError() {
		t.Fatalf("commit: %s", toolResultText(commitRes))
	}
	logRes := NewGitLogTool().Execute(ctx, "c3", []byte(`{"path":"`+dir+`","limit":5}`), nil)
	if logRes.IsError() || !strings.Contains(toolResultText(logRes), "second") {
		t.Fatalf("expected second commit in log: %+v %q", logRes.IsError(), toolResultText(logRes))
	}
}
