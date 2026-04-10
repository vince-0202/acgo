package harness

import (
	"context"
	"path/filepath"
	"testing"
)

func TestResolveToolPath_AbsoluteUnchanged(t *testing.T) {
	ctx := ContextWithToolWorkingDir(context.Background(), "/tmp/proj")
	p := ResolveToolPath(ctx, "/other/file.go")
	if filepath.Clean("/other/file.go") != p {
		t.Fatalf("got %q", p)
	}
}

func TestResolveToolPath_RelativeJoinsWd(t *testing.T) {
	ctx := ContextWithToolWorkingDir(context.Background(), "/tmp/proj")
	p := ResolveToolPath(ctx, "foo/bar.go")
	want := filepath.Join("/tmp", "proj", "foo", "bar.go")
	if filepath.Clean(want) != p {
		t.Fatalf("want %q got %q", want, p)
	}
}

func TestResolveToolPath_NoWdRelativeUnchanged(t *testing.T) {
	p := ResolveToolPath(context.Background(), "foo.go")
	if p != "foo.go" {
		t.Fatalf("got %q", p)
	}
}
