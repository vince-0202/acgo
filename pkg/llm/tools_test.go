package llm

import (
	"encoding/json"
	"testing"
)

func TestNormalizeToolCallArguments(t *testing.T) {
	tests := []struct {
		name string
		raw  json.RawMessage
		want string
	}{
		{"nil", nil, "{}"},
		{"empty", json.RawMessage(``), "{}"},
		{"empty with spaces", json.RawMessage(`   `), "{}"},
		{"valid object", json.RawMessage(`{"path":"/tmp"}`), `{"path":"/tmp"}`},
		{"valid array", json.RawMessage(`["a","b"]`), `["a","b"]`},
		{"double-encoded string", json.RawMessage(`"{\"path\":\"/tmp\"}"`), `{"path":"/tmp"}`},
		{"double-encoded with spaces", json.RawMessage(` "{\"x\":1}" `), `{"x":1}`},
		{"object with spaces", json.RawMessage(`  {"a":1}  `), `{"a":1}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeToolCallArguments(tt.raw)
			if string(got) != tt.want {
				t.Errorf("NormalizeToolCallArguments() = %q, want %q", string(got), tt.want)
			}
			// Result should be valid JSON (object or array) when non-empty expectation
			if tt.want != "{}" && tt.want != "" {
				var _ json.RawMessage = got
				if !json.Valid(got) {
					t.Errorf("NormalizeToolCallArguments() returned invalid JSON: %q", string(got))
				}
			}
		})
	}
}

func TestToolCallAccumulator_Build(t *testing.T) {
	acc := &ToolCallAccumulator{ID: "call-1", Name: "read"}
	acc.AppendArgumentsDelta(`{"path":"`)
	acc.AppendArgumentsDelta(`/tmp"}`)
	tc := acc.Build()
	if tc.ID != "call-1" || tc.Name != "read" {
		t.Errorf("Build() id/name = %q/%q, want call-1/read", tc.ID, tc.Name)
	}
	want := `{"path":"/tmp"}`
	if string(tc.Arguments) != want {
		t.Errorf("Build() Arguments = %q, want %q", string(tc.Arguments), want)
	}
}

func TestToolCallAccumulator_EmptyArgs(t *testing.T) {
	acc := &ToolCallAccumulator{ID: "call-2", Name: "write"}
	tc := acc.Build()
	if string(tc.Arguments) != "{}" {
		t.Errorf("Build() with no deltas should normalize to {}; got %q", string(tc.Arguments))
	}
}
