package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCoerceToolArguments_singleRequiredString(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string"},
		},
		"required": []string{"path"},
	}
	coerced := CoerceToolArguments(schema, json.RawMessage(`"/tmp/x"`))
	var m map[string]any
	if err := json.Unmarshal(coerced, &m); err != nil {
		t.Fatal(err)
	}
	if m["path"] != "/tmp/x" {
		t.Fatalf("got %#v", m)
	}
}

func TestCoerceToolArguments_listPathFromBareString(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string"},
			"glob": map[string]any{"type": "string"},
		},
		"required": []string{},
	}
	coerced := CoerceToolArguments(schema, json.RawMessage(`"."`))
	var m map[string]any
	if err := json.Unmarshal(coerced, &m); err != nil {
		t.Fatal(err)
	}
	if m["path"] != "." {
		t.Fatalf("got %#v", m)
	}
}

func TestValidateToolArguments_requiredMissing(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{"type": "string", "minLength": 1},
		},
		"required": []string{"command"},
	}
	err := ValidateToolArguments("bash", schema, json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "bash") {
		t.Fatal(err)
	}
}

func TestValidateToolArguments_ok(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{"type": "string", "minLength": 1},
		},
		"required": []string{"command"},
	}
	err := ValidateToolArguments("bash", schema, json.RawMessage(`{"command":"echo hi"}`))
	if err != nil {
		t.Fatal(err)
	}
}
