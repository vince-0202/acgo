// Skills: two layers — system ~/.acgo/skills and project <cwd>/.acgo/skills —
// each holds subfolders named <skill-name>/SKILL.md. YAML frontmatter + body
// are parsed; full text is merged via SkillsController (ContextController.Load).
package harness

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

// SkillsController discovers skills from disk and merges them into the system prompt.
// It mirrors the controller pattern used elsewhere in harness (e.g. ContextController.Load).
type SkillsController struct {
	workDir string
	Skills  []Skill
	Paths   []string
}

// NewSkillsController creates a controller for the given working directory
// (typically process CWD so project .acgo/skills is discovered).
func NewSkillsController(workDir string) *SkillsController {
	return &SkillsController{workDir: workDir}
}

// Load reads system (~/.acgo/skills) then project (<workDir>/.acgo/skills).
// Project skills override system skills when the name collides.
func (sc *SkillsController) Load() {
	if sc == nil {
		return
	}
	sc.Skills, sc.Paths = Load(sc.workDir)
}

// SystemPrompt appends a "## Skills" section with the full body of each SKILL.md
// so the model has skill instructions in context without a separate tool.
func (sc *SkillsController) SystemPrompt() string {
	var b strings.Builder
	b.WriteString("\n\n## Skills\n\n")
	b.WriteString("Two layers: (1) system `~/.acgo/skills/<skill-name>/SKILL.md`; ")
	b.WriteString("(2) project `<project-root>/.acgo/skills/<skill-name>/SKILL.md`. ")
	b.WriteString("Project skills override system skills for the same name. ")
	b.WriteString("Apply a skill when it matches the user's task.\n\n")
	if sc == nil || len(sc.Skills) == 0 {
		b.WriteString("(none)\n")
		return b.String()
	}
	for i, s := range sc.Skills {
		if i > 0 {
			b.WriteString("\n---\n\n")
		}
		b.WriteString("### Skill: " + s.Name + "\n")
		if strings.TrimSpace(s.Description) != "" {
			b.WriteString("**Summary:** " + strings.TrimSpace(s.Description) + "\n\n")
		}
		if strings.TrimSpace(s.Path) != "" {
			b.WriteString("**Source:** `" + strings.TrimSpace(s.Path) + "`\n\n")
		}
		b.WriteString(strings.TrimSpace(s.Content))
		b.WriteString("\n")
	}
	return b.String()
}

// Load discovers skills from two directories only:
//  1. system:  ~/.acgo/skills  (user home)
//  2. project: <workDir>/.acgo/skills  (typically current project root / cwd)
//
// Same skill name in project overrides system. Returns merged list and loaded file paths.
func Load(workDir string) ([]Skill, []string) {
	byName := make(map[string]Skill)
	var paths []string

	home, _ := os.UserHomeDir()
	systemSkillsDir := filepath.Join(home, ".acgo", "skills")
	collectSkillsFromDir(systemSkillsDir, byName, &paths)

	if root := absProjectRoot(workDir); root != "" {
		projectSkillsDir := filepath.Join(root, ".acgo", "skills")
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

// absProjectRoot returns a clean absolute path for the project directory used
// to resolve .acgo/skills. Empty workDir yields "".
func absProjectRoot(workDir string) string {
	s := strings.TrimSpace(workDir)
	if s == "" {
		return ""
	}
	abs, err := filepath.Abs(s)
	if err != nil || abs == "" {
		return ""
	}
	return filepath.Clean(abs)
}
