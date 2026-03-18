// Package skills loads SKILL.md files from ~/.acgo/skills and .acgo/skills
// (from workDir upward), parses YAML frontmatter + body, and merges them
// into the agent system prompt (Cursor/agentskills-style).
package skills

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const skillFileName = "SKILL.md"

// Skill is a single skill: name, description, and markdown body.
type Skill struct {
	Name        string
	Description string
	Content     string
	Path        string // path to SKILL.md for debugging/listing
}

// Load discovers and parses skills from global (~/.acgo/skills) and
// project (.acgo/skills in dirs from root toward workDir).
// Later directories override earlier for the same skill name.
// Returns skills (deduplicated by name, project overrides global) and paths of loaded files.
func Load(workDir string) ([]Skill, []string) {
	byName := make(map[string]Skill)
	var paths []string

	home, _ := os.UserHomeDir()
	globalSkillsDir := filepath.Join(home, ".acgo", "skills")
	collectSkillsFromDir(globalSkillsDir, byName, &paths)

	for _, d := range dirsFromRootToCwd(workDir) {
		projectSkillsDir := filepath.Join(d, ".acgo", "skills")
		collectSkillsFromDir(projectSkillsDir, byName, &paths)
	}

	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]Skill, 0, len(names))
	for _, name := range names {
		out = append(out, byName[name])
	}
	return out, paths
}

func collectSkillsFromDir(skillsDir string, byName map[string]Skill, paths *[]string) {
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		skillPath := filepath.Join(skillsDir, e.Name(), skillFileName)
		b, err := os.ReadFile(skillPath)
		if err != nil {
			continue
		}
		*paths = append(*paths, skillPath)
		s, ok := parseSkillMD(string(b), skillPath)
		if !ok || strings.TrimSpace(s.Name) == "" {
			// Fallback: allow SKILL.md without YAML frontmatter.
			// Use directory name as the skill name, and infer a short description from body.
			body := strings.TrimSpace(string(b))
			desc := ""
			if body != "" {
				// take first paragraph as description
				if idx := strings.Index(body, "\n\n"); idx > 0 {
					desc = strings.TrimSpace(body[:idx])
				} else {
					desc = body
				}
				if len(desc) > 200 {
					desc = desc[:200] + "..."
				}
			}
			byName[e.Name()] = Skill{
				Name:        e.Name(),
				Description: desc,
				Content:     body,
				Path:        skillPath,
			}
			continue
		}
		byName[strings.TrimSpace(s.Name)] = s
	}
}

// parseSkillMD parses SKILL.md content: YAML frontmatter between --- and ---, then body.
// Returns (skill, true) or (zero value, false) on parse failure.
func parseSkillMD(content, path string) (Skill, bool) {
	const delim = "---"
	content = strings.TrimSuffix(content, "\n")
	first := strings.Index(content, delim)
	if first < 0 {
		return Skill{}, false
	}
	afterFirst := strings.TrimPrefix(content[len(delim):], "\n")
	second := strings.Index(afterFirst, delim)
	if second < 0 {
		return Skill{}, false
	}
	front := strings.TrimSpace(afterFirst[:second])
	body := strings.TrimSpace(afterFirst[second+len(delim):])

	name, desc := parseFrontmatter(front)
	if name == "" {
		return Skill{}, false
	}
	if desc == "" && body != "" {
		// use first paragraph of body as description fallback
		if idx := strings.Index(body, "\n\n"); idx > 0 {
			desc = strings.TrimSpace(body[:idx])
		} else {
			desc = strings.TrimSpace(body)
			if len(desc) > 200 {
				desc = desc[:200] + "..."
			}
		}
	}
	return Skill{
		Name:        name,
		Description: desc,
		Content:     body,
		Path:        path,
	}, true
}

func parseFrontmatter(s string) (name, description string) {
	lines := strings.Split(s, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if idx := strings.Index(line, ":"); idx > 0 {
			key := strings.TrimSpace(strings.ToLower(line[:idx]))
			val := strings.TrimSpace(line[idx+1:])
			val = strings.Trim(val, "\"'")
			switch key {
			case "name":
				name = val
			case "description":
				description = val
			}
		}
	}
	return name, description
}

// dirsFromRootToCwd returns directories from filesystem root toward workDir
// so that workDir is last (project overrides).
func dirsFromRootToCwd(workDir string) []string {
	abs, err := filepath.Abs(workDir)
	if err != nil || abs == "" {
		return nil
	}
	abs = filepath.Clean(abs)
	var parts []string
	for {
		parts = append(parts, abs)
		parent := filepath.Dir(abs)
		if parent == abs {
			break
		}
		abs = parent
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return parts
}

// MergePrompt appends an "Available skills" section to existingPrompt.
// It intentionally does NOT inline full skill content; the model should use tools
// (e.g. list/read) to open the referenced SKILL.md paths on demand.
func MergePrompt(existingPrompt string, skillList []Skill) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(existingPrompt))
	b.WriteString("\n\n## Available skills\n\n")
	b.WriteString("Skills are stored as folders containing a SKILL.md.\n")
	b.WriteString("Base paths:\n")
	b.WriteString("- ~/.acgo/skills/<skill-name>/SKILL.md\n")
	b.WriteString("- .acgo/skills/<skill-name>/SKILL.md (from project root up to current workdir)\n\n")
	b.WriteString("Do NOT assume skill details from the title/description. If you need a skill, use tools to open its SKILL.md path and follow it.\n\n")
	if len(skillList) == 0 {
		b.WriteString("(none)\n")
		return b.String()
	}
	for i, s := range skillList {
		if i > 0 {
			b.WriteString("\n---\n\n")
		}
		b.WriteString("## Skill: " + s.Name + "\n")
		if s.Description != "" {
			b.WriteString("- description: " + strings.TrimSpace(s.Description) + "\n")
		}
		if strings.TrimSpace(s.Path) != "" {
			b.WriteString("- path: " + strings.TrimSpace(s.Path) + "\n")
		}
	}
	return b.String()
}
