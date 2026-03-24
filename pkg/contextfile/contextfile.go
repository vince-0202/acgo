// Package contextfile loads AGENTS.md, SYSTEM.md, and APPEND_SYSTEM.md from
// global (~/.acgo) and project directories (from cwd upward), then merges
// them into a single system prompt (aligned with pi-mono behavior).
package contextfile

import (
	"os"
	"path/filepath"
	"strings"
)

const defaultSystemPrompt = "You are a helpful coding assistant."

// Status holds the merged system prompt and paths of files that were loaded.
type Status struct {
	Prompt string   // Final merged system prompt
	Paths  []string // File paths that were read (for logging/debug)
}

// Load merges context files from global and project directories.
// workDir is typically os.Getwd(); from workDir we walk upward to root.
// Merge order: default → global ~/.acgo → project dirs (root … → workDir)
// so that the directory closest to workDir wins for SYSTEM.md replacement.
func Load(workDir string) *Status {
	var prompt = defaultSystemPrompt
	var paths []string

	// 1) Apply global directory
	prompt, paths = applyDir(workDir, prompt, paths)

	// 2) Dirs from root toward workDir so workDir has highest priority
	dirs := dirsFromRootToCwd(workDir)
	for _, d := range dirs {
		prompt, paths = applyDir(d, prompt, paths)
	}

	return &Status{Prompt: strings.TrimSpace(prompt), Paths: paths}
}

// applyDir reads SYSTEM.md (replaces), AGENTS.md and APPEND_SYSTEM.md (append) from dir.
func applyDir(dir, current string, paths []string) (string, []string) {
	// SYSTEM.md: replace
	if b, p := readFile(dir, "SYSTEM.md"); b != "" {
		current = strings.TrimSpace(b)
		paths = append(paths, p)
	}
	// AGENTS.md: append
	if b, p := readFile(dir, "AGENTS.md"); b != "" {
		current = current + "\n\n" + strings.TrimSpace(b)
		paths = append(paths, p)
	}
	// APPEND_SYSTEM.md: append
	if b, p := readFile(dir, "APPEND_SYSTEM.md"); b != "" {
		current = current + "\n\n" + strings.TrimSpace(b)
		paths = append(paths, p)
	}
	return current, paths
}

func readFile(dir, name string) (content string, path string) {
	path = filepath.Join(dir, name)
	b, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	return string(b), path
}

// dirsFromRootToCwd returns directories from filesystem root toward workDir
// (e.g. ["/", "/home", "/home/user", "/home/user/proj"] so workDir wins when we apply in order).
func dirsFromRootToCwd(workDir string) []string {
	abs, err := filepath.Abs(workDir)
	if err != nil || abs == "" {
		return nil
	}
	abs = filepath.Clean(abs)
	var parts []string
	for {
		parts = append(parts, abs)
		parent := filepath.Dir(abs)
		if parent == abs {
			break
		}
		abs = parent
	}
	// parts is [workDir, parent, ..., root]; reverse to get root ... workDir
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return parts
}
