package tui

import (
	"strings"

	"github.com/vince-0202/acgo/pkg/llm"
)

func providerName(p llm.Provider) string {
	if p == nil {
		return "unknown"
	}
	name := strings.TrimSpace(p.Name())
	if name == "" {
		return "unknown"
	}
	return name
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func ternary[T any](cond bool, a, b T) T {
	if cond {
		return a
	}
	return b
}
