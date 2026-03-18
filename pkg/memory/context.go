package memory

import "context"

type sessionIDContextKey struct{}

var sessionIDKey sessionIDContextKey

// WithSessionID stores the current session identifier in context so memory tools
// and writers can scope writes/reads by session.
func WithSessionID(ctx context.Context, sessionID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, sessionIDKey, sessionID)
}

// SessionIDFromContext tries to read session_id from ctx.
func SessionIDFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	v := ctx.Value(sessionIDKey)
	if v == nil {
		return "", false
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return "", false
	}
	return s, true
}
