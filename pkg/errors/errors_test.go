package errors

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestClassifyError(t *testing.T) {
	if ClassifyError(nil) != ErrKindNone {
		t.Errorf("ClassifyError(nil) = %q", ClassifyError(nil))
	}
	if ClassifyError(context.Canceled) != ErrKindAborted {
		t.Errorf("ClassifyError(Canceled) = %q", ClassifyError(context.Canceled))
	}
	if ClassifyError(context.DeadlineExceeded) != ErrKindAborted {
		t.Errorf("ClassifyError(DeadlineExceeded) = %q", ClassifyError(context.DeadlineExceeded))
	}
	wrapped := fmt.Errorf("wrapped: %w", context.Canceled)
	if ClassifyError(wrapped) != ErrKindAborted {
		t.Errorf("ClassifyError(wrapped Canceled) = %q", ClassifyError(wrapped))
	}
	if ClassifyError(fmt.Errorf("api error")) != ErrKindLLM {
		t.Errorf("ClassifyError(generic) = %q", ClassifyError(fmt.Errorf("api error")))
	}
	ae := &AgentError{Err: fmt.Errorf("tool failed"), Kind: ErrKindTool}
	if ClassifyError(ae) != ErrKindTool {
		t.Errorf("ClassifyError(AgentError Tool) = %q", ClassifyError(ae))
	}
}

func TestWrapAgentError(t *testing.T) {
	if WrapAgentError(nil, ErrKindLLM) != nil {
		t.Error("WrapAgentError(nil) should return nil")
	}
	err := fmt.Errorf("fail")
	w := WrapAgentError(err, ErrKindTool)
	var ae *AgentError
	if !errors.As(w, &ae) || ae.Kind != ErrKindTool || ae.Err != err {
		t.Errorf("WrapAgentError: ae=%v", ae)
	}
	if w.Error() != err.Error() {
		t.Errorf("WrapAgentError.Error() = %q", w.Error())
	}
}

func TestFormatErrorForDisplay(t *testing.T) {
	if FormatErrorForDisplay(nil) != "" {
		t.Error("FormatErrorForDisplay(nil) should be empty")
	}
	if s := FormatErrorForDisplay(fmt.Errorf("plain")); s != "plain" {
		t.Errorf("FormatErrorForDisplay(plain) = %q", s)
	}
	ae := &AgentError{Err: fmt.Errorf("context canceled"), Kind: ErrKindAborted}
	if s := FormatErrorForDisplay(ae); s != "已取消: context canceled" {
		t.Errorf("FormatErrorForDisplay(AgentError) = %q", s)
	}
}
