package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xeipuuv/gojsonschema"
)

// ToolValidationError is returned when tool arguments do not satisfy the tool's JSON Schema.
// Prefer returning this from the validation layer; the agent turns it into an isError toolResult
// for the model (see executePendingTools), rather than aborting the turn.
type ToolValidationError struct {
	ToolName string
	Details  []string
}

func (e *ToolValidationError) Error() string {
	if e == nil {
		return ""
	}
	msg := fmt.Sprintf("tool argument validation failed for %s", e.ToolName)
	if len(e.Details) == 0 {
		return msg
	}
	return msg + ": " + strings.Join(e.Details, "; ")
}

// CoerceToolArguments normalizes common model mistakes before JSON Schema validation:
//   - If args are a JSON string and the schema has exactly one required string property,
//     wrap as a single-field object (e.g. "/path" -> {"path":"/path"}).
//   - If there are no required properties but the schema defines "path", a bare string
//     is wrapped as {"path": "..."} (list directory tool).
func CoerceToolArguments(schema map[string]any, args json.RawMessage) json.RawMessage {
	if len(args) == 0 {
		return json.RawMessage(`{}`)
	}
	var raw any
	if err := json.Unmarshal(args, &raw); err != nil {
		return args
	}
	if m, ok := raw.(map[string]any); ok && m != nil {
		return args
	}
	s, ok := raw.(string)
	if !ok {
		return args
	}

	req := requiredKeys(schema)
	props, _ := schema["properties"].(map[string]any)

	if len(req) == 1 && props != nil {
		key := req[0]
		if prop, ok := props[key].(map[string]any); ok {
			if typ, _ := prop["type"].(string); typ == "string" {
				out, err := json.Marshal(map[string]string{key: s})
				if err == nil {
					return out
				}
			}
		}
	}

	if len(req) == 0 && props != nil {
		if _, hasPath := props["path"]; hasPath {
			out, err := json.Marshal(map[string]string{"path": s})
			if err == nil {
				return out
			}
		}
	}

	return args
}

// ValidateToolArguments validates args against schema using JSON Schema Draft-7 semantics
// (github.com/xeipuuv/gojsonschema, AJV-style). schema must describe a JSON object (type: object).
func ValidateToolArguments(toolName string, schema map[string]any, args json.RawMessage) error {
	if schema == nil {
		schema = map[string]any{"type": "object"}
	}
	// Ensure document is JSON; empty normalizes to {}.
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	var probe any
	if err := json.Unmarshal(args, &probe); err != nil {
		return &ToolValidationError{ToolName: toolName, Details: []string{"arguments must be valid JSON: " + err.Error()}}
	}

	schemaLoader := gojsonschema.NewGoLoader(schema)
	docLoader := gojsonschema.NewBytesLoader(args)
	result, err := gojsonschema.Validate(schemaLoader, docLoader)
	if err != nil {
		return &ToolValidationError{ToolName: toolName, Details: []string{err.Error()}}
	}
	if result.Valid() {
		return nil
	}
	var details []string
	for _, e := range result.Errors() {
		details = append(details, e.String())
	}
	return &ToolValidationError{ToolName: toolName, Details: details}
}

func requiredKeys(schema map[string]any) []string {
	req, ok := schema["required"]
	if !ok || req == nil {
		return nil
	}
	switch x := req.(type) {
	case []string:
		return append([]string(nil), x...)
	case []any:
		var out []string
		for _, v := range x {
			if s, ok := v.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}
