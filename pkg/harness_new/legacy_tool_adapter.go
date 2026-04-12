package harness_new

import (
	"context"
	"encoding/json"

	"github.com/vince-0202/acgo/pkg/agent_new"
	"github.com/vince-0202/acgo/pkg/communi"
	"github.com/vince-0202/acgo/pkg/harness"
)

type legacyToolAdapter struct {
	tool harness.Tool
}

func AdaptLegacyTool(tool harness.Tool) agent_new.Tool {
	if tool == nil {
		return nil
	}
	return &legacyToolAdapter{tool: tool}
}

func AdaptLegacyTools(tools ...harness.Tool) []agent_new.Tool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]agent_new.Tool, 0, len(tools))
	for _, tool := range tools {
		if adapted := AdaptLegacyTool(tool); adapted != nil {
			out = append(out, adapted)
		}
	}
	return out
}

func (a *legacyToolAdapter) Name() string {
	return a.tool.Name()
}

func (a *legacyToolAdapter) Label() string {
	return a.tool.Label()
}

func (a *legacyToolAdapter) Description() string {
	return a.tool.Description()
}

func (a *legacyToolAdapter) JSONSchema() map[string]any {
	return a.tool.JSONSchema()
}

func (a *legacyToolAdapter) Execute(ctx context.Context, toolCallID string, args json.RawMessage, update agent_new.ToolUpdateFunc) communi.ToolCallResult {
	if update == nil {
		return a.tool.Execute(ctx, toolCallID, args, nil)
	}
	return a.tool.Execute(ctx, toolCallID, args, func(in harness.ToolUpdate) {
		update(agent_new.ToolUpdate{
			Text:     in.Text,
			Progress: in.Progress,
			Metadata: in.Metadata,
		})
	})
}
