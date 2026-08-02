package stormbreak

import (
	"errors"
	"strings"
	"testing"
)

func TestRetryErrorFormattingAndUnwrap(t *testing.T) {
	last := errors.New("dependency failed")
	tests := []struct {
		name     string
		err      *RetryError
		contains string
	}{
		{name: "empty", err: &RetryError{Attempts: 1}, contains: "stopped after 1 attempt(s)"},
		{name: "cause", err: &RetryError{Attempts: 2, Cause: ErrBudgetExhausted}, contains: ErrBudgetExhausted.Error()},
		{name: "last", err: &RetryError{Attempts: 2, LastError: last}, contains: "last error: dependency failed"},
		{name: "both", err: &RetryError{Attempts: 3, Cause: ErrAttemptsExhausted, LastError: last}, contains: "last error: dependency failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.err.Error(); !strings.Contains(got, test.contains) {
				t.Fatalf("Error() = %q, want substring %q", got, test.contains)
			}
		})
	}
	both := tests[len(tests)-1].err
	if !errors.Is(both, ErrAttemptsExhausted) || !errors.Is(both, last) {
		t.Fatal("RetryError did not unwrap cause and last error")
	}
	var nilRetryError *RetryError
	if nilRetryError.Error() != "<nil>" || nilRetryError.Unwrap() != nil {
		t.Fatal("nil RetryError methods are not safe")
	}
}
