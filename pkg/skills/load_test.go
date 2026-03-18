package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSkillMD(t *testing.T) {
	content := `---
name: code-review
description: Review code for quality and security. Use when the user asks for a code review.
---

# Code Review

## Checklist
- Logic and edge cases
- Security (SQL injection, XSS)
`
	s, ok := parseSkillMD(content, "/fake/path/SKILL.md")
	if !ok {
		t.Fatal("parseSkillMD failed")
	}
	if s.Name != "code-review" {
		t.Errorf("Name: got %q", s.Name)
	}
	if s.Description != "Review code for quality and security. Use when the user asks for a code review." {
		t.Errorf("Description: got %q", s.Description)
	}
	if !strings.Contains(s.Content, "## Checklist") {
		t.Errorf("Content missing Checklist: %q", s.Content)
	}
	if s.Path != "/fake/path/SKILL.md" {
		t.Errorf("Path: got %q", s.Path)
	}
}

func TestParseSkillMD_NoDescription(t *testing.T) {
	content := `---
name: my-skill
---

First paragraph here.

More content.
`
	s, ok := parseSkillMD(content, "")
	if !ok {
		t.Fatal("parseSkillMD failed")
	}
	if s.Name != "my-skill" {
		t.Errorf("Name: got %q", s.Name)
	}
	if s.Description != "First paragraph here." {
		t.Errorf("Description fallback: got %q", s.Description)
	}
}

func TestMergePrompt(t *testing.T) {
	base := "You are a helpful assistant."
	skills := []Skill{
		{Name: "a", Description: "Desc A", Content: "Content A", Path: "/x/a/SKILL.md"},
		{Name: "b", Description: "Desc B", Content: "Content B", Path: "/x/b/SKILL.md"},
	}
	out := MergePrompt(base, skills)
	if !strings.Contains(out, base) {
		t.Error("base prompt missing")
	}
	if !strings.Contains(out, "## Available skills") {
		t.Error("Available skills section missing")
	}
	if !strings.Contains(out, "## Skill: a") {
		t.Error("Skill a block missing")
	}
	if strings.Contains(out, "Content A") {
		t.Error("should not inline skill content")
	}
	if !strings.Contains(out, "## Skill: b") {
		t.Error("Skill b block missing")
	}
	if !strings.Contains(out, "/x/a/SKILL.md") || !strings.Contains(out, "/x/b/SKILL.md") {
		t.Error("skill paths missing")
	}
}

func TestMergePrompt_EmptySkills(t *testing.T) {
	base := "Hello."
	out := MergePrompt(base, nil)
	if !strings.Contains(out, "## Available skills") || !strings.Contains(out, "(none)") {
		t.Errorf("expected empty skills section, got %q", out)
	}
	out = MergePrompt(base, []Skill{})
	if !strings.Contains(out, "## Available skills") || !strings.Contains(out, "(none)") {
		t.Errorf("expected empty skills section with empty slice, got %q", out)
	}
}

func TestLoad_Integration(t *testing.T) {
	dir := t.TempDir()
	oldHome := os.Getenv("HOME")
	t.Cleanup(func() { _ = os.Setenv("HOME", oldHome) })
	_ = os.Setenv("HOME", dir)
	skillDir := filepath.Join(dir, ".acgo", "skills", "test-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	md := `---
name: test-skill
description: A test skill.
---

# Test

Body here.
`
	if err := os.WriteFile(filepath.Join(skillDir, skillFileName), []byte(md), 0644); err != nil {
		t.Fatal(err)
	}
	workDir := dir
	skills, paths := Load(workDir)
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Name != "test-skill" {
		t.Errorf("skill name: got %q", skills[0].Name)
	}
	if len(paths) < 1 {
		t.Fatalf("expected at least 1 path, got %d", len(paths))
	}
	for _, p := range paths {
		if filepath.Base(p) != skillFileName {
			t.Errorf("path: got %q", p)
		}
	}
}

func TestLoad_Integration_NoFrontmatter(t *testing.T) {
	dir := t.TempDir()
	oldHome := os.Getenv("HOME")
	t.Cleanup(func() { _ = os.Setenv("HOME", oldHome) })
	_ = os.Setenv("HOME", dir)
	skillDir := filepath.Join(dir, ".acgo", "skills", "no-frontmatter")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	md := `# Title

First paragraph.

More text.
`
	if err := os.WriteFile(filepath.Join(skillDir, skillFileName), []byte(md), 0644); err != nil {
		t.Fatal(err)
	}
	skills, _ := Load(dir)
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Name != "no-frontmatter" {
		t.Errorf("name: got %q", skills[0].Name)
	}
	if !strings.Contains(skills[0].Description, "Title") {
		t.Errorf("description should be inferred, got %q", skills[0].Description)
	}
}
