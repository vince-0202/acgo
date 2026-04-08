package tui

import (
	"testing"

	"github.com/vince-0202/acgo/pkg/harness"
)

func TestPermissionModeLabelMapping(t *testing.T) {
	cases := []struct {
		mode harness.PermissionMode
		want string
	}{
		{mode: harness.PermissionModeDefault, want: "default"},
		{mode: harness.PermissionModeAcceptEdits, want: "acceptEdits"},
		{mode: harness.PermissionModePlan, want: "plan"},
		{mode: harness.PermissionModeAuto, want: "auto"},
		{mode: harness.PermissionModeBypass, want: "bypassPermissions"},
		{mode: harness.PermissionMode("unexpected"), want: "default"},
	}
	for _, tc := range cases {
		if got := permissionModeLabel(tc.mode); got != tc.want {
			t.Fatalf("permissionModeLabel(%q)=%q, want %q", tc.mode, got, tc.want)
		}
	}
}

func TestNextPermissionModeLabel(t *testing.T) {
	if got := nextPermissionModeLabel("default"); got != "acceptEdits" {
		t.Fatalf("default next=%q, want acceptEdits", got)
	}
	if got := nextPermissionModeLabel("acceptEdits"); got != "plan" {
		t.Fatalf("acceptEdits next=%q, want plan", got)
	}
	if got := nextPermissionModeLabel("plan"); got != "auto" {
		t.Fatalf("plan next=%q, want auto", got)
	}
	if got := nextPermissionModeLabel("auto"); got != "bypassPermissions" {
		t.Fatalf("auto next=%q, want bypassPermissions", got)
	}
	if got := nextPermissionModeLabel("bypassPermissions"); got != "default" {
		t.Fatalf("bypassPermissions next=%q, want default", got)
	}
}
