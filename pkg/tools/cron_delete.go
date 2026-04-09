package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/vince-0202/acgo/pkg/runtime"
	"strings"

	"github.com/vince-0202/acgo/pkg/communi"
	"github.com/vince-0202/acgo/pkg/harness"
)

type cronDeleteTool struct{}

func (t *cronDeleteTool) Name() string  { return "cron_delete" }
func (t *cronDeleteTool) Label() string { return "Cron Delete" }
func (t *cronDeleteTool) Description() string {
	return "Delete a scheduled cron task by id."
}

func (t *cronDeleteTool) JSONSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": map[string]any{
				"type":        "string",
				"minLength":   1,
				"description": "Cron task id to delete.",
			},
		},
		"required": []string{"id"},
	}
}

func (t *cronDeleteTool) Execute(ctx context.Context, toolCallID string, args json.RawMessage, update harness.ToolUpdateFunc) communi.ToolCallResult {
	var params struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return communi.ErrorToolCallResult(toolCallID, err)
	}
	id := strings.TrimSpace(params.ID)
	if id == "" {
		return communi.ErrorToolCallResult(toolCallID, fmt.Errorf("id is required"))
	}
	if !runtime.DefaultCronManager.Delete(id) {
		return communi.ErrorToolCallResult(toolCallID, fmt.Errorf("cron task not found: %s", id))
	}
	return communi.NewToolCallResult(toolCallID, "deleted cron task "+id)
}

func NewCronDeleteTool() harness.Tool {
	return &cronDeleteTool{}
}
