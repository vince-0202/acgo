package session

import (
	"fmt"
	"github.com/vince-0202/acgo/pkg/config"
	"github.com/vince-0202/acgo/pkg/session"
	"path/filepath"
)

func LoadSession(sessionId string, settings config.SessionConfig) (*session.Session, error) {
	var sess *session.Session
	if sessionId == "" {
		path, err := session.NewSessionPath(settings.Root)
		if err != nil {
			return nil, fmt.Errorf("create session path: %w", err)
		}
		sess, err = session.Create(path)
		if err != nil {
			return nil, fmt.Errorf("create session: %w", err)
		}
	} else {
		path := filepath.Join(settings.Root, sessionId+".jsonl")
		sess = session.Open(path)
		if err := sess.LoadMessage(); err != nil {
			return nil, err
		}
	}
	return sess, nil
}
