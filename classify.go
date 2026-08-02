package stormbreak

import (
	"errors"
	"fmt"
)

// Classifier reports whether an operation error is retryable.
type Classifier func(error) bool

type permanentError struct{ err error }

func (e *permanentError) Error() string { return fmt.Sprintf("stormbreak: permanent error: %v", e.err) }
func (e *permanentError) Unwrap() error { return e.err }

// Permanent marks err as non-retryable. A nil error remains nil.
func Permanent(err error) error {
	if err == nil || IsPermanent(err) {
		return err
	}
	return &permanentError{err: err}
}

// IsPermanent reports whether err or one of its wrapped errors was marked permanent.
func IsPermanent(err error) bool {
	var target *permanentError
	return errors.As(err, &target)
}

// NeverRetry rejects every error.
func NeverRetry(error) bool { return false }

// AlwaysRetry accepts every non-nil error.
func AlwaysRetry(err error) bool { return err != nil }
