package session

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/vince-0202/acgo/pkg/keys"
	"github.com/vince-0202/acgo/pkg/utils"
)

// Message is a single entry in a session log.
type Message struct {
	ID         string         `json:"id"`
	ParentID   string         `json:"parent_id,omitempty"`
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	Thinking   string         `json:"thinking,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	IsError    bool           `json:"is_error,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// Session manages appending and reading messages from a JSONL file.
type Session struct {
	Path string
}

// Create creates a new session file at path (overwriting if it exists).
func Create(path string) (*Session, error) {
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		return nil, err
	}
	return &Session{Path: path}, nil
}

// Open opens an existing session file.
func Open(path string) *Session {
	return &Session{Path: path}
}

// AppendMessage appends a message as a single JSON line.
func (s *Session) AppendMessage(msg Message) error {
	f, err := os.OpenFile(s.Path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	if err := enc.Encode(msg); err != nil {
		return err
	}
	return nil
}

// LoadAll reads all messages from the session file.
func (s *Session) LoadAll() ([]Message, error) {
	f, err := os.Open(s.Path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var result []Message
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var msg Message
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			continue
		}
		result = append(result, msg)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// NewSessionPath returns a new session file path under root with format <snowflake_id>.jsonl.
func NewSessionPath(root string) (string, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	id := utils.NextID(keys.IdKindSnowflake)
	return filepath.Join(root, id+".jsonl"), nil
}

// List returns paths of session files in root (JSONL files), newest first by modification time.
func List(root string) ([]string, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".jsonl" {
			continue
		}
		paths = append(paths, filepath.Join(root, e.Name()))
	}
	// newest first by ModTime (session files are named by snowflake ID)
	sortPathsByModTime(paths)
	return paths, nil
}

func sortPathsByModTime(paths []string) {
	type pathTime struct {
		path string
		mod  int64
	}
	pts := make([]pathTime, len(paths))
	for i, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			pts[i] = pathTime{p, 0}
			continue
		}
		pts[i] = pathTime{p, info.ModTime().UnixNano()}
	}
	sort.Slice(pts, func(i, j int) bool { return pts[j].mod < pts[i].mod }) // newest first
	for i := range paths {
		paths[i] = pts[i].path
	}
}
