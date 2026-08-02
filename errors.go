package stormbreak

import (
	"errors"
	"fmt"
)

var (
	// ErrBudgetExhausted indicates that shared retry capacity was unavailable.
	ErrBudgetExhausted = errors.New("stormbreak: retry budget exhausted")
	// ErrAttemptsExhausted indicates that every policy attempt was used.
	ErrAttemptsExhausted = errors.New("stormbreak: retry attempts exhausted")
	// ErrInvalidPolicy indicates invalid retry policy configuration.
	ErrInvalidPolicy = errors.New("stormbreak: invalid retry policy")
	// ErrInvalidBudget indicates invalid retry budget configuration.
	ErrInvalidBudget = errors.New("stormbreak: invalid retry budget configuration")
)

// RetryError describes why a retry operation stopped.
type RetryError struct {
	Attempts  int
	LastError error
	Cause     error
}

// Error returns a summary containing the attempt count, terminal cause, and last error.
func (e *RetryError) Error() string {
	if e == nil {
		return "<nil>"
	}
	switch {
	case e.Cause == nil && e.LastError == nil:
		return fmt.Sprintf("stormbreak: stopped after %d attempt(s)", e.Attempts)
	case e.Cause == nil:
		return fmt.Sprintf("stormbreak: stopped after %d attempt(s): last error: %v", e.Attempts, e.LastError)
	case e.LastError == nil:
		return fmt.Sprintf("stormbreak: stopped after %d attempt(s): %v", e.Attempts, e.Cause)
	default:
		return fmt.Sprintf("stormbreak: stopped after %d attempt(s): %v: last error: %v", e.Attempts, e.Cause, e.LastError)
	}
}

// Unwrap exposes both the terminal cause and last operation error to errors.Is/As.
func (e *RetryError) Unwrap() error {
	if e == nil {
		return nil
	}
	return errors.Join(e.Cause, e.LastError)
}
