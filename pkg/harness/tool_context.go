package harness

import (
	"context"
	"path/filepath"
	"strings"
)

type toolWorkingDirKey struct{}

// ContextWithToolWorkingDir attaches the directory tools should use as cwd / relative-path base.
func ContextWithToolWorkingDir(ctx context.Context, dir string) context.Context {
	if ctx == nil || strings.TrimSpace(dir) == "" {
		return ctx
	}
	return context.WithValue(ctx, toolWorkingDirKey{}, filepath.Clean(dir))
}

// ToolWorkingDirFromContext returns the tool working directory, or "" if unset.
func ToolWorkingDirFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v := ctx.Value(toolWorkingDirKey{})
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// ResolveToolPath resolves a path for file tools: absolute paths stay as-is; relative paths join the tool working directory when set.
func ResolveToolPath(ctx context.Context, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	wd := ToolWorkingDirFromContext(ctx)
	if wd == "" {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(wd, path))
}
