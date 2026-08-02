package stormbreak

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"reflect"
	"time"
)

// Do executes operation immediately and gates every subsequent attempt through budget.
func Do[T any](ctx context.Context, budget Budget, policy Policy, operation func(context.Context) (T, error), opts ...Option) (T, error) {
	var zero T
	if ctx == nil {
		return zero, fmt.Errorf("stormbreak: context cannot be nil")
	}
	if isNilBudget(budget) {
		return zero, fmt.Errorf("%w: budget cannot be nil", ErrInvalidBudget)
	}
	if operation == nil {
		return zero, fmt.Errorf("stormbreak: operation cannot be nil")
	}
	if err := policy.Validate(); err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}

	settings := options{random: rand.Float64, wait: wait}
	for _, option := range opts {
		if option != nil {
			option(&settings)
		}
	}
	if settings.classifier == nil {
		settings.classifier = defaultClassifier
	}
	if settings.random == nil {
		return zero, fmt.Errorf("%w: random source cannot be nil", ErrInvalidPolicy)
	}
	if settings.wait == nil {
		return zero, fmt.Errorf("%w: wait function cannot be nil", ErrInvalidPolicy)
	}

	started := time.Now()
	for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		if settings.hooks.OnAttempt != nil {
			settings.hooks.OnAttempt(AttemptEvent{Attempt: attempt})
		}
		result, err := operation(ctx)
		if err != nil && settings.hooks.OnFailure != nil {
			settings.hooks.OnFailure(FailureEvent{Attempt: attempt, Error: err})
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return zero, ctxErr
		}
		if err == nil {
			if settings.hooks.OnSuccess != nil {
				settings.hooks.OnSuccess(SuccessEvent{Attempts: attempt, Elapsed: time.Since(started)})
			}
			return result, nil
		}
		if IsPermanent(err) || !settings.classifier(err) {
			return zero, err
		}
		if attempt == policy.MaxAttempts {
			return zero, &RetryError{Attempts: attempt, LastError: err, Cause: ErrAttemptsExhausted}
		}
		if !budget.Allow() {
			remaining := budget.Remaining()
			if settings.hooks.OnBudgetExhausted != nil {
				settings.hooks.OnBudgetExhausted(BudgetEvent{Attempts: attempt, LastError: err, Remaining: remaining})
			}
			return zero, &RetryError{Attempts: attempt, LastError: err, Cause: ErrBudgetExhausted}
		}
		delay := backoff(policy, attempt, settings.random)
		if settings.hooks.OnRetry != nil {
			settings.hooks.OnRetry(RetryEvent{Attempt: attempt + 1, Delay: delay, Error: err, BudgetRemaining: budget.Remaining()})
		}
		if err := settings.wait(ctx, delay); err != nil {
			return zero, err
		}
	}
	panic("stormbreak: unreachable retry state")
}

func isNilBudget(budget Budget) bool {
	switch typed := budget.(type) {
	case nil:
		return true
	case *TokenBudget:
		return typed == nil
	}
	value := reflect.ValueOf(budget)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func defaultClassifier(err error) bool {
	return err != nil && !IsPermanent(err) && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

// DoVoid is the error-only form of Do.
func DoVoid(ctx context.Context, budget Budget, policy Policy, operation func(context.Context) error, options ...Option) error {
	if operation == nil {
		return fmt.Errorf("stormbreak: operation cannot be nil")
	}
	_, err := Do(ctx, budget, policy, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, operation(ctx)
	}, options...)
	return err
}
