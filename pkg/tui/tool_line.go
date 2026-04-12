package tui

import (
	"encoding/json"
	"sort"
	"strings"
)

func formatToolLine(toolName string, status string) string {
	return formatToolLineWithArgs(toolName, nil, status)
}

func formatToolLineWithArgs(toolName string, args []byte, status string) string {
	base := formatToolBase(toolName, args)
	status = strings.TrimSpace(status)
	if status == "" {
		return base
	}
	return base + " · " + status
}

func formatToolBase(toolName string, args []byte) string {
	name := strings.TrimSpace(toolName)
	if name == "" {
		name = "unknown"
	}
	detail := strings.TrimSpace(toolExecuteDetailForDisplay(name, args))
	if detail == "" {
		return "Tool > " + name
	}
	return "Tool > " + name + " " + detail
}

func toolExecuteDetailForDisplay(toolName string, args []byte) string {
	toolName = strings.ToLower(strings.TrimSpace(toolName))
	argText := strings.TrimSpace(string(args))
	if argText == "" || argText == "null" {
		return ""
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(args, &m); err != nil {
		var s string
		if json.Unmarshal(args, &s) == nil && strings.TrimSpace(s) != "" {
			return truncateOneLine(s, 300)
		}
		return truncateOneLine(argText, 300)
	}
	jsonString := func(key string) string {
		v, ok := m[key]
		if !ok {
			return ""
		}
		var s string
		if json.Unmarshal(v, &s) != nil {
			return ""
		}
		return strings.TrimSpace(s)
	}

	switch toolName {
	case "read", "write", "edit":
		if p := jsonString("path"); p != "" {
			return truncateOneLine(p, 400)
		}
	case "bash":
		if c := jsonString("command"); c != "" {
			return truncateOneLine(c, 400)
		}
	case "list":
		dir := jsonString("path")
		if dir == "" {
			dir = "."
		}
		if g := jsonString("glob"); g != "" {
			return truncateOneLine(dir+" glob="+g, 400)
		}
		return truncateOneLine(dir, 400)
	case "grep":
		pat := jsonString("pattern")
		root := jsonString("path")
		if root == "" {
			root = "."
		}
		if pat != "" {
			return truncateOneLine(pat+" in "+root, 400)
		}
	case "rag_search", "memory_recall":
		if q := jsonString("query"); q != "" {
			return truncateOneLine(q, 300)
		}
	}
	return compactJSONArgsForDisplay(m, 300)
}

func compactJSONArgsForDisplay(m map[string]json.RawMessage, maxLen int) string {
	skip := map[string]bool{
		"content":      true,
		"instructions": true,
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		if skip[k] {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		var s string
		if json.Unmarshal(m[k], &s) == nil && strings.TrimSpace(s) != "" {
			parts = append(parts, k+"="+truncateOneLine(s, 120))
			continue
		}
		raw := strings.TrimSpace(string(m[k]))
		if len(raw) > 60 {
			raw = raw[:57] + "..."
		}
		parts = append(parts, k+":"+raw)
	}
	return truncateOneLine(strings.Join(parts, ", "), maxLen)
}

func truncateOneLine(s string, max int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.Join(strings.Fields(s), " ")
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
