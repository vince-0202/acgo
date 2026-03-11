package llm

import "fmt"

// ErrUnknownProvider is returned when no provider is registered for a given name.
type ErrUnknownProvider string

func (e ErrUnknownProvider) Error() string {
	return fmt.Sprintf("unknown provider: %s", string(e))
}

