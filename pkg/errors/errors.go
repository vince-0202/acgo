package errors

import (
	"context"
	"errors"
)

// ErrKind classifies agent errors for UI and logging.
type ErrKind string

const (
	ErrKindNone    ErrKind = ""        // no error
	ErrKindLLM     ErrKind = "llm"     // LLM provider / stream failed
	ErrKindTool    ErrKind = "tool"    // tool execution failed
	ErrKindContext ErrKind = "context" // context / transform / config error
	ErrKindAborted ErrKind = "aborted" // user cancel or timeout
)

// String returns a short label for the error kind (for display).
func (k ErrKind) String() string {
	if k == "" {
		return ""
	}
	switch k {
	case ErrKindLLM:
		return "LLM 调用失败"
	case ErrKindTool:
		return "工具执行失败"
	case ErrKindContext:
		return "上下文错误"
	case ErrKindAborted:
		return "已取消"
	default:
		return string(k)
	}
}

// AgentError wraps an error with a kind for EventAgentEnd / EventTurnEnd and Prompt return.
type AgentError struct {
	Err  error
	Kind ErrKind
}

func (e *AgentError) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e *AgentError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// ClassifyError maps an error to ErrKind (nil -> ErrKindNone).
// Use when the source is unknown; otherwise set kind explicitly at emit sites.
func ClassifyError(err error) ErrKind {
	if err == nil {
		return ErrKindNone
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return ErrKindAborted
	}
	var ae *AgentError
	if errors.As(err, &ae) && ae != nil && ae.Kind != ErrKindNone {
		return ae.Kind
	}
	return ErrKindLLM
}

// WrapAgentError returns err wrapped with kind for return from Prompt; returns nil if err is nil.
func WrapAgentError(err error, kind ErrKind) error {
	if err == nil {
		return nil
	}
	if kind == ErrKindNone {
		kind = ClassifyError(err)
	}
	return &AgentError{Err: err, Kind: kind}
}

// FormatErrorForDisplay returns a short, user-facing string for the error (e.g. "已取消: context canceled").
func FormatErrorForDisplay(err error) string {
	if err == nil {
		return ""
	}
	var ae *AgentError
	if errors.As(err, &ae) && ae != nil && ae.Err != nil {
		if ae.Kind != ErrKindNone {
			return ae.Kind.String() + ": " + ae.Err.Error()
		}
	}
	return err.Error()
}
