package session

import (
	"fmt"
	"path/filepath"

	"github.com/vince-0202/acgo/pkg/config"
	"github.com/vince-0202/acgo/pkg/session"
)

func LoadSession(sessionID string, settings config.SessionConfig) (*session.Session, error) {
	var sess *session.Session
	if sessionID == "" {
		path, err := session.NewSessionPath(settings.Root)
		if err != nil {
			return nil, fmt.Errorf("create session path: %w", err)
		}
		sess, err = session.Create(path)
		if err != nil {
			return nil, fmt.Errorf("create session: %w", err)
		}
		return sess, nil
	}

	path := filepath.Join(settings.Root, sessionID+".jsonl")
	sess = session.Open(path)
	if err := sess.LoadMessage(); err != nil {
		return nil, err
	}
	return sess, nil
}
