package session

import (
	"path/filepath"
	"testing"
)

func TestNewSessionPath_and_List(t *testing.T) {
	root := t.TempDir()
	p1, err := NewSessionPath(root)
	if err != nil {
		t.Fatalf("NewSessionPath: %v", err)
	}
	if filepath.Dir(p1) != root {
		t.Errorf("path dir = %q", filepath.Dir(p1))
	}
	if filepath.Ext(p1) != ".jsonl" {
		t.Errorf("path ext = %q", filepath.Ext(p1))
	}
	p2, err := NewSessionPath(root)
	if err != nil {
		t.Fatalf("NewSessionPath 2: %v", err)
	}
	if p1 == p2 {
		t.Error("expected different paths")
	}
	// Create session files so List finds them
	if _, err := Create(p1); err != nil {
		t.Fatalf("Create p1: %v", err)
	}
	if _, err := Create(p2); err != nil {
		t.Fatalf("Create p2: %v", err)
	}
	paths, err := List(root)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(paths) != 2 {
		t.Errorf("List: got %d paths", len(paths))
	}
}

func TestCreate_AppendMessage_LoadAll(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	s, err := Create(path)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := s.AppendMessage(Message{ID: "1", Role: "user", Content: "hi"}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	all, err := s.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(all) != 1 || all[0].Content != "hi" {
		t.Errorf("LoadAll: %v", all)
	}
}
