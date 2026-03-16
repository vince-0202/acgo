package session

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Message is a single entry in a session log.
type Message struct {
	ID        string         `json:"id"`
	ParentID  string         `json:"parent_id,omitempty"`
	Role      string         `json:"role"`
	Content   string         `json:"content"`
	CreatedAt time.Time      `json:"created_at"`
	Metadata  map[string]any `json:"metadata,omitempty"`
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

// NewSessionPath returns a new session file path under root with format YYYYMMDD-HHMMSS-<random>.jsonl.
func NewSessionPath(root string) (string, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	now := time.Now().UTC()
	prefix := now.Format("20060102-150405")
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return filepath.Join(root, fmt.Sprintf("%s-%s.jsonl", prefix, hex.EncodeToString(b))), nil
}

// List returns paths of session files in root (JSONL files), newest first by name.
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
	// newest first (filename sort gives chronological order for YYYYMMDD-HHMMSS-*)
	for i, j := 0, len(paths)-1; i < j; i, j = i+1, j-1 {
		paths[i], paths[j] = paths[j], paths[i]
	}
	return paths, nil
}
