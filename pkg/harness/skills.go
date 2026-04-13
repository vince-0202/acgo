package harness

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/vince-0202/acgo/pkg/agent"
)

const skillFileName = "SKILL.md"

type skillDoc struct {
	Name        string
	Description string
	Content     string
	Path        string
}

type SkillsController struct {
	agent         agent.AgentRuntime
	skills        []skillDoc
	lastSkillText string
}

func NewSkillsController() *SkillsController {
	return &SkillsController{}
}

func (sc *SkillsController) Name() string {
	return "skills"
}

func (sc *SkillsController) Install(runtime agent.AgentRuntime) (func(), error) {
	sc.agent = runtime
	sc.applySkillsPrompt()
	unsub := runtime.Subscribe(func(event agent.Event, abort func()) {
		if event.Type == agent.EventAgentStart {
			sc.applySkillsPrompt()
		}
	})
	return func() {
		unsub()
		sc.agent = nil
		sc.skills = nil
		sc.lastSkillText = ""
	}, nil
}

func (sc *SkillsController) applySkillsPrompt() {
	if sc == nil || sc.agent == nil {
		return
	}
	workDir := strings.TrimSpace(sc.agent.ContextManager().ToolWorkingDirectory())
	sc.skills = loadSkills(workDir)
	nextSkillText := strings.TrimSpace(skillsPrompt(sc.skills))

	prompt := sc.agent.ContextManager().SystemPrompt()
	trimLast := strings.TrimSpace(sc.lastSkillText)
	if trimLast != "" {
		promptTrim := strings.TrimSpace(prompt)
		if strings.HasSuffix(promptTrim, trimLast) {
			keep := strings.TrimSpace(strings.TrimSuffix(promptTrim, trimLast))
			prompt = keep
		}
	}

	if nextSkillText != "" {
		if strings.TrimSpace(prompt) == "" {
			prompt = nextSkillText
		} else {
			prompt = strings.TrimSpace(prompt) + "\n\n" + nextSkillText
		}
	}
	sc.agent.ContextManager().ReplacePrompt(prompt)
	sc.lastSkillText = nextSkillText
}

func skillsPrompt(skills []skillDoc) string {
	var b strings.Builder
	b.WriteString("\n\n## Skills\n\n")
	b.WriteString("Two layers: (1) system `~/.acgo/skills/<skill-name>/SKILL.md`; ")
	b.WriteString("(2) project `<project-root>/.acgo/skills/<skill-name>/SKILL.md`. ")
	b.WriteString("Project skills override system skills for the same name. ")
	b.WriteString("Apply a skill when it matches the user's task.\n\n")
	if len(skills) == 0 {
		b.WriteString("(none)\n")
		return b.String()
	}
	for i, s := range skills {
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

func loadSkills(workDir string) []skillDoc {
	byName := make(map[string]skillDoc)

	home, _ := os.UserHomeDir()
	collectSkillsFromDir(filepath.Join(home, ".acgo", "skills"), byName)

	if root := absProjectRoot(workDir); root != "" {
		collectSkillsFromDir(filepath.Join(root, ".acgo", "skills"), byName)
	}

	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]skillDoc, 0, len(names))
	for _, name := range names {
		out = append(out, byName[name])
	}
	return out
}

func collectSkillsFromDir(skillsDir string, byName map[string]skillDoc) {
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillPath := filepath.Join(skillsDir, entry.Name(), skillFileName)
		body, err := os.ReadFile(skillPath)
		if err != nil {
			continue
		}
		doc, ok := parseSkillMD(string(body), skillPath)
		if !ok || strings.TrimSpace(doc.Name) == "" {
			byName[entry.Name()] = skillDoc{
				Name:        entry.Name(),
				Description: summarizeSkillBody(string(body)),
				Content:     strings.TrimSpace(string(body)),
				Path:        skillPath,
			}
			continue
		}
		byName[strings.TrimSpace(doc.Name)] = doc
	}
}

func parseSkillMD(content, path string) (skillDoc, bool) {
	const delim = "---"
	content = strings.TrimSuffix(content, "\n")
	if !strings.HasPrefix(content, delim) {
		return skillDoc{}, false
	}
	rest := strings.TrimPrefix(content, delim)
	rest = strings.TrimPrefix(rest, "\n")
	second := strings.Index(rest, delim)
	if second < 0 {
		return skillDoc{}, false
	}
	front := strings.TrimSpace(rest[:second])
	body := strings.TrimSpace(rest[second+len(delim):])

	name, desc := parseFrontmatter(front)
	if name == "" {
		return skillDoc{}, false
	}
	if desc == "" {
		desc = summarizeSkillBody(body)
	}
	return skillDoc{
		Name:        name,
		Description: desc,
		Content:     body,
		Path:        path,
	}, true
}

func parseFrontmatter(front string) (name, description string) {
	for _, line := range strings.Split(front, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		idx := strings.Index(line, ":")
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(strings.ToLower(line[:idx]))
		val := strings.Trim(strings.TrimSpace(line[idx+1:]), "\"'")
		switch key {
		case "name":
			name = val
		case "description":
			description = val
		}
	}
	return name, description
}

func summarizeSkillBody(body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	if idx := strings.Index(body, "\n\n"); idx > 0 {
		body = strings.TrimSpace(body[:idx])
	}
	if len(body) > 200 {
		return body[:200] + "..."
	}
	return body
}

func absProjectRoot(workDir string) string {
	workDir = strings.TrimSpace(workDir)
	if workDir == "" {
		return ""
	}
	abs, err := filepath.Abs(workDir)
	if err != nil || abs == "" {
		return ""
	}
	return filepath.Clean(abs)
}
