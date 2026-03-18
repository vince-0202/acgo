package tools

import (
	"acgo/pkg/agent"
	"acgo/pkg/skills"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// skillSearchTool exposes skill discovery/loading via a tool.
// This avoids inlining skill contents into the system prompt.
type skillSearchTool struct{}

func (t *skillSearchTool) Name() string  { return "skill_search" }
func (t *skillSearchTool) Label() string { return "Skill Search" }
func (t *skillSearchTool) Description() string {
	return "List available skills and load a skill's SKILL.md content. Skills live under ~/.acgo/skills/<name>/SKILL.md or .acgo/skills/<name>/SKILL.md. Use to discover skills or read a specific skill by name."
}

func (t *skillSearchTool) JSONSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "Optional skill name. If provided, returns that skill's SKILL.md content; otherwise lists available skills.",
			},
			"work_dir": map[string]any{
				"type":        "string",
				"description": "Optional working directory for project skill discovery. Defaults to current process working directory.",
			},
		},
	}
}

func (t *skillSearchTool) Execute(ctx context.Context, toolCallID string, args json.RawMessage, update agent.ToolUpdateFunc) (agent.ToolResult, error) {
	var params struct {
		Name    string `json:"name"`
		WorkDir string `json:"work_dir"`
	}
	_ = json.Unmarshal(args, &params)

	workDir := strings.TrimSpace(params.WorkDir)
	if workDir == "" {
		if wd, err := os.Getwd(); err == nil {
			workDir = wd
		}
	}

	skillList, _ := skills.Load(workDir)
	if strings.TrimSpace(params.Name) == "" {
		if len(skillList) == 0 {
			return agent.ToolResult{Content: "no skills found"}, nil
		}
		var b strings.Builder
		b.WriteString("skills:\n")
		for _, s := range skillList {
			b.WriteString("  - " + s.Name + "\n")
			if strings.TrimSpace(s.Description) != "" {
				b.WriteString("    description: " + strings.TrimSpace(s.Description) + "\n")
			}
			if strings.TrimSpace(s.Path) != "" {
				b.WriteString("    path: " + strings.TrimSpace(s.Path) + "\n")
			}
		}
		return agent.ToolResult{Content: strings.TrimSpace(b.String())}, nil
	}

	name := strings.TrimSpace(params.Name)
	for _, s := range skillList {
		if s.Name == name {
			if strings.TrimSpace(s.Path) == "" {
				return agent.ToolResult{Content: "skill found but path missing: " + name, IsError: true}, nil
			}
			data, err := os.ReadFile(s.Path)
			if err != nil {
				return agent.ToolResult{Content: err.Error(), IsError: true}, nil
			}
			return agent.ToolResult{
				Content: string(data),
				Metadata: map[string]any{
					"name": name,
					"path": s.Path,
				},
			}, nil
		}
	}
	return agent.ToolResult{Content: fmt.Sprintf("skill not found: %s", name), IsError: true}, nil
}

// NewSkillSearchTool creates a new skill_search AgentTool.
func NewSkillSearchTool() agent.AgentTool {
	return &skillSearchTool{}
}
