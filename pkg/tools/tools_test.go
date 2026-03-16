package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEditTool_WriteAndReadBack(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	edit := NewEditTool()
	ctx := context.Background()

	// Edit creates the file
	res, err := edit.Execute(ctx, "call-1", []byte(`{"path":"`+path+`","content":"hello\nworld"}`), nil)
	if err != nil {
		t.Fatalf("edit execute: %v", err)
	}
	if res.IsError {
		t.Fatalf("edit returned IsError: %s", res.Content)
	}

	// Read back via read tool
	read := NewReadTool()
	res2, err := read.Execute(ctx, "call-2", []byte(`{"path":"`+path+`"}`), nil)
	if err != nil {
		t.Fatalf("read execute: %v", err)
	}
	if res2.IsError {
		t.Fatalf("read returned IsError: %s", res2.Content)
	}
	if res2.Content != "hello\nworld" {
		t.Errorf("read content = %q", res2.Content)
	}

	// Edit again (overwrite)
	res3, err := edit.Execute(ctx, "call-3", []byte(`{"path":"`+path+`","content":"bye"}`), nil)
	if err != nil {
		t.Fatalf("edit execute 2: %v", err)
	}
	if res3.IsError {
		t.Fatalf("edit 2 returned IsError: %s", res3.Content)
	}
	res4, _ := read.Execute(ctx, "call-4", []byte(`{"path":"`+path+`"}`), nil)
	if res4.Content != "bye" {
		t.Errorf("after second edit read content = %q", res4.Content)
	}
}

func TestEditTool_InvalidArgs(t *testing.T) {
	edit := NewEditTool()
	ctx := context.Background()
	_, err := edit.Execute(ctx, "call-1", []byte(`{}`), nil)
	if err == nil {
		t.Error("expected error for missing path")
	}
	_, err = edit.Execute(ctx, "call-2", []byte(`{"path":"/tmp/x","content":1}`), nil)
	if err == nil {
		t.Error("expected error for invalid JSON")
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
	res, err := grep.Execute(ctx, "call-1", []byte(`{"pattern":"func main","path":"`+dir+`"}`), nil)
	if err != nil {
		t.Fatalf("grep execute: %v", err)
	}
	if res.IsError {
		t.Fatalf("grep IsError: %s", res.Content)
	}
	if res.Content == "" || res.Content == "no matches found" {
		t.Errorf("grep should find match: %q", res.Content)
	}
}

func TestListTool_ListDir(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "a.go"), []byte("x"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "b.txt"), []byte("y"), 0644)
	list := NewListTool()
	ctx := context.Background()
	res, err := list.Execute(ctx, "call-1", []byte(`{"path":"`+dir+`"}`), nil)
	if err != nil {
		t.Fatalf("list execute: %v", err)
	}
	if res.IsError {
		t.Fatalf("list IsError: %s", res.Content)
	}
	if res.Content == "" {
		t.Error("list should return entries")
	}
	res2, _ := list.Execute(ctx, "call-2", []byte(`{"path":"`+dir+`","glob":"*.go"}`), nil)
	if res2.IsError {
		t.Fatalf("list glob IsError: %s", res2.Content)
	}
	if res2.Content == "" || !strings.Contains(res2.Content, "a.go") || !strings.Contains(res2.Content, "1 entries") {
		t.Errorf("list glob = %q", res2.Content)
	}
}

func TestReadTool_FileNotFound_IsError(t *testing.T) {
	read := NewReadTool()
	ctx := context.Background()
	res, err := read.Execute(ctx, "call-1", []byte(`{"path":"/nonexistent/file"}`), nil)
	if err != nil {
		t.Fatalf("read should return ToolResult for file error, not Go error: %v", err)
	}
	if !res.IsError {
		t.Error("read should set IsError for file not found")
	}
}

func TestBashTool_CommandFails_IsError(t *testing.T) {
	bash := NewBashTool()
	ctx := context.Background()
	res, err := bash.Execute(ctx, "call-1", []byte(`{"command":"exit 1"}`), nil)
	if err != nil {
		t.Fatalf("bash should return ToolResult for command failure: %v", err)
	}
	if !res.IsError {
		t.Error("bash should set IsError when command fails")
	}
}
