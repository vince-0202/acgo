package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vince-0202/acgo/pkg/agent"
	"github.com/vince-0202/acgo/pkg/communi"
)

func toolResultText(tr communi.ToolCallResult) string {
	m := tr.ToMessage()
	return (&m).ContentBlocksToText()
}

func TestEditTool_WriteAndReadBack(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	edit := NewEditTool()
	ctx := context.Background()

	// Edit creates the file
	res := edit.Execute(ctx, "call-1", []byte(`{"path":"`+path+`","content":"hello\nworld"}`), nil)
	if res.IsError() {
		t.Fatalf("edit returned IsError: %s", toolResultText(res))
	}

	// Read back via read tool
	read := NewReadTool()
	res2 := read.Execute(ctx, "call-2", []byte(`{"path":"`+path+`"}`), nil)
	if res2.IsError() {
		t.Fatalf("read returned IsError: %s", toolResultText(res2))
	}
	if toolResultText(res2) != "hello\nworld" {
		t.Errorf("read content = %q", toolResultText(res2))
	}

	// Edit again (overwrite)
	res3 := edit.Execute(ctx, "call-3", []byte(`{"path":"`+path+`","content":"bye"}`), nil)
	if res3.IsError() {
		t.Fatalf("edit 2 returned IsError: %s", toolResultText(res3))
	}
	res4 := read.Execute(ctx, "call-4", []byte(`{"path":"`+path+`"}`), nil)
	if toolResultText(res4) != "bye" {
		t.Errorf("after second edit read content = %q", toolResultText(res4))
	}
}

func TestEditTool_InvalidArgs(t *testing.T) {
	edit := NewEditTool()
	ctx := context.Background()
	if err := agent.ValidateToolArguments(edit.Name(), edit.JSONSchema(), []byte(`{}`)); err == nil {
		t.Error("expected validation error for missing path")
	}
	res := edit.Execute(ctx, "call-2", []byte(`{"path":"/tmp/x","content":1}`), nil)
	if !res.IsError() {
		t.Error("expected error result for invalid JSON content type")
	}
}

func TestGrepTool_FindsMatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.go")
	if err := os.WriteFile(path, []byte("package main\nfunc main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	grep := NewGrepTool()
	ctx := context.Background()
	res := grep.Execute(ctx, "call-1", []byte(`{"pattern":"func main","path":"`+dir+`"}`), nil)
	if res.IsError() {
		t.Fatalf("grep IsError: %s", toolResultText(res))
	}
	txt := toolResultText(res)
	if txt == "" || txt == "no matches found" {
		t.Errorf("grep should find match: %q", txt)
	}
}

func TestListTool_ListDir(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "a.go"), []byte("x"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "b.txt"), []byte("y"), 0644)
	list := NewListTool()
	ctx := context.Background()
	res := list.Execute(ctx, "call-1", []byte(`{"path":"`+dir+`"}`), nil)
	if res.IsError() {
		t.Fatalf("list IsError: %s", toolResultText(res))
	}
	if toolResultText(res) == "" {
		t.Error("list should return entries")
	}
	res2 := list.Execute(ctx, "call-2", []byte(`{"path":"`+dir+`","glob":"*.go"}`), nil)
	if res2.IsError() {
		t.Fatalf("list glob IsError: %s", toolResultText(res2))
	}
	t2 := toolResultText(res2)
	if t2 == "" || !strings.Contains(t2, "a.go") || !strings.Contains(t2, "1 entries") {
		t.Errorf("list glob = %q", t2)
	}
}

func TestReadTool_FileNotFound_IsError(t *testing.T) {
	read := NewReadTool()
	ctx := context.Background()
	res := read.Execute(ctx, "call-1", []byte(`{"path":"/nonexistent/file"}`), nil)
	if !res.IsError() {
		t.Error("read should set IsError for file not found")
	}
}

func TestBashTool_CommandFails_IsError(t *testing.T) {
	bash := NewBashTool()
	ctx := context.Background()
	res := bash.Execute(ctx, "call-1", []byte(`{"command":"exit 1"}`), nil)
	if !res.IsError() {
		t.Error("bash should set IsError when command fails")
	}
}
