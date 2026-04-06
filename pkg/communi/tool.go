package communi

import (
	"encoding/json"
	"github.com/vince-0202/acgo/pkg/keys"
)

// ToolSchema describes a callable function exposed to the model.
// JSONSchema is a generic JSON Schema for the parameters, compatible with gojsonschema.
type ToolSchema struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	JSONSchema  map[string]any `json:"json_schema"`
}

// ToolCallRequest represents a model-requested tool invocation.
//
// Arguments convention: always JSON (object or array) as raw bytes. During streaming
// it may be partial; use NormalizeToolCallArguments before execution to get a
// stable, executable payload (handles empty, nil, and double-encoded string).
type ToolCallRequest struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"` // raw JSON; may be partial during streaming
}

func NewToolCallErrorMessage(toolCallId string, err error) Message {
	return Message{
		ID:      "tool-" + toolCallId,
		Role:    keys.AgentRoleTool,
		Content: []*ContentBlock{NewTextContentBlock(err.Error())},
		ToolCall: &ToolCallRequest{
			ID: toolCallId,
		},
		IsError: true,
	}
}

func ErrorToolCallResult(toolCallId string, err error) ToolCallResult {
	return ToolCallResult{
		ToolCallID: toolCallId,
		Content:    []*ContentBlock{NewTextContentBlock(err.Error())},
		Error:      err,
	}
}

func NewToolCallResult(toolCallId, context string) ToolCallResult {
	return ToolCallResult{
		ToolCallID: toolCallId,
		Content:    []*ContentBlock{NewTextContentBlock(context)},
		Error:      nil,
	}
}

func NewToolCallResultWithMetaData(toolCallId, context string, metaData map[string]any) ToolCallResult {
	return ToolCallResult{
		ToolCallID: toolCallId,
		Content:    []*ContentBlock{NewTextContentBlock(context)},
		Metadata:   metaData,
		Error:      nil,
	}
}

type ToolCallResult struct {
	ToolCallID string
	Content    []*ContentBlock // text content summarizing the result
	Error      error
	Metadata   map[string]any // optional metadata
}

func (tr ToolCallResult) ToMessage() Message {
	return Message{
		ID:      "tool-" + tr.ToolCallID,
		Role:    keys.AgentRoleTool,
		Content: tr.Content,
		ToolCall: &ToolCallRequest{
			ID: tr.ToolCallID,
		},
		IsError: tr.Error != nil,
	}
}

func (tr ToolCallResult) IsError() bool {
	return tr.Error != nil
}
