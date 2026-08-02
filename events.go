package stormbreak

import "time"

// AttemptEvent is emitted immediately before an operation attempt.
type AttemptEvent struct{ Attempt int }

// RetryEvent is emitted after a retry token is consumed and before backoff.
type RetryEvent struct {
	Attempt         int
	Delay           time.Duration
	Error           error
	BudgetRemaining int64
}

// SuccessEvent is emitted after an operation succeeds.
type SuccessEvent struct {
	Attempts int
	Elapsed  time.Duration
}

// FailureEvent is emitted when an operation attempt fails.
type FailureEvent struct {
	Attempt int
	Error   error
}

// BudgetEvent is emitted when a retry cannot proceed because the budget is empty.
type BudgetEvent struct {
	Attempts  int
	LastError error
	Remaining int64
}

// Hooks contains optional synchronous observability callbacks.
// Hook functions should return quickly and must not block critical paths.
type Hooks struct {
	OnAttempt         func(AttemptEvent)
	OnRetry           func(RetryEvent)
	OnSuccess         func(SuccessEvent)
	OnFailure         func(FailureEvent)
	OnBudgetExhausted func(BudgetEvent)
}
