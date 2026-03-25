package agent

import (
	"context"
	"encoding/json"
	"github.com/vince-0202/acgo/pkg/llm"
)

// ToolUpdate describes an incremental update from a running tool.
type ToolUpdate struct {
	Text     string         // human readable update text
	Progress float64        // optional progress 0..1
	Metadata map[string]any // arbitrary metadata
}

// ToolUpdateFunc is used by tools to report streaming progress.
type ToolUpdateFunc func(update ToolUpdate)

// ToolResult is the final result of a tool execution.
type ToolResult struct {
	Content  string         // text content summarizing the result
	IsError  bool           // whether the tool failed
	Metadata map[string]any // optional metadata
}

// Tool execution and argument conventions:
//
// 1) JSON Schema: each tool must return a Draft-7 style object schema from JSONSchema().
// The agent runs CoerceToolArguments (bare string → single required string field when applicable),
// then ValidateToolArguments (gojsonschema) before Execute.
//
// 2) Validation failure: the agent does not call Execute; it appends a tool message with
// IsError=true and Content describing the schema errors (see ToolValidationError).
//
// 3) Return values from Execute:
//   - Operational failure the model should see (bad path, non-zero exit, etc.): return
//     (ToolResult{Content: "...", IsError: true}, nil). Do not use non-nil error for these.
//   - Programming or contract violation inside the tool: return (zero ToolResult, err);
//     the agent wraps err into an IsError tool result for the model.

// AgentTool is the interface implemented by all tools usable by the Agent.
type AgentTool interface {
	Name() string
	Label() string
	Description() string
	JSONSchema() map[string]any
	Execute(ctx context.Context, toolCallID string, args json.RawMessage, update ToolUpdateFunc) (ToolResult, error)
}

// agentToolsToLlm converts AgentTools to llm.Tool slice for provider options.
func agentToolsToLlm(tools []AgentTool) []llm.Tool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]llm.Tool, 0, len(tools))
	for _, t := range tools {
		out = append(out, llm.Tool{
			Name:        t.Name(),
			Description: t.Description(),
			JSONSchema:  t.JSONSchema(),
		})
	}
	return out
}
