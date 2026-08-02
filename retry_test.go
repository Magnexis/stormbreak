package stormbreak

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func testPolicy(attempts int) Policy {
	return Policy{MaxAttempts: attempts, BaseDelay: 0, MaxDelay: 0, Multiplier: 1}
}

func withWaitFunction(waiter func(context.Context, time.Duration) error) Option {
	return func(options *options) { options.wait = waiter }
}

func TestDoSuccessFirstAttempt(t *testing.T) {
	budget, _ := NewBudget(Config{Capacity: 1})
	result, err := Do(context.Background(), budget, testPolicy(2), func(context.Context) (string, error) { return "ok", nil })
	if err != nil || result != "ok" || budget.Remaining() != 1 {
		t.Fatalf("result=%q err=%v remaining=%d", result, err, budget.Remaining())
	}
}

func TestDoSuccessfulRetry(t *testing.T) {
	budget, _ := NewBudget(Config{Capacity: 2})
	var calls int
	result, err := Do(context.Background(), budget, testPolicy(3), func(context.Context) (int, error) {
		calls++
		if calls == 1 {
			return 0, errors.New("temporary")
		}
		return 42, nil
	})
	if err != nil || result != 42 || calls != 2 || budget.Remaining() != 1 {
		t.Fatalf("result=%d err=%v calls=%d remaining=%d", result, err, calls, budget.Remaining())
	}
}

func TestDoAttemptsExhausted(t *testing.T) {
	budget, _ := NewBudget(Config{Capacity: 3})
	operationErr := errors.New("down")
	_, err := Do(context.Background(), budget, testPolicy(3), func(context.Context) (int, error) { return 0, operationErr })
	var retryErr *RetryError
	if !errors.Is(err, ErrAttemptsExhausted) || !errors.Is(err, operationErr) || !errors.As(err, &retryErr) || retryErr.Attempts != 3 {
		t.Fatalf("unexpected error: %#v", err)
	}
}

func TestDoBudgetExhausted(t *testing.T) {
	budget, _ := NewBudget(Config{Capacity: 1})
	_, err := Do(context.Background(), budget, testPolicy(4), func(context.Context) (int, error) { return 0, errors.New("down") })
	var retryErr *RetryError
	if !errors.Is(err, ErrBudgetExhausted) || !errors.As(err, &retryErr) || retryErr.Attempts != 2 {
		t.Fatalf("unexpected error: %#v", err)
	}
}

func TestDoContextCanceledBeforeExecution(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	budget, _ := NewBudget(Config{Capacity: 1})
	called := false
	_, err := Do(ctx, budget, testPolicy(2), func(context.Context) (int, error) { called = true; return 0, nil })
	if !errors.Is(err, context.Canceled) || called {
		t.Fatalf("err=%v called=%v", err, called)
	}
}

func TestDoContextCancellationWinsAfterOperationReturns(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	budget, _ := NewBudget(Config{Capacity: 1})
	successHook := false
	result, err := Do(ctx, budget, testPolicy(1), func(context.Context) (int, error) {
		cancel()
		return 42, nil
	}, WithHooks(Hooks{OnSuccess: func(SuccessEvent) { successHook = true }}))
	if !errors.Is(err, context.Canceled) || result != 0 || successHook {
		t.Fatalf("result=%d err=%v successHook=%v", result, err, successHook)
	}
}

func TestDoContextCanceledDuringBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	budget, _ := NewBudget(Config{Capacity: 2})
	policy := Policy{MaxAttempts: 3, BaseDelay: time.Hour, MaxDelay: time.Hour, Multiplier: 1}
	_, err := Do(ctx, budget, policy, func(context.Context) (int, error) { return 0, errors.New("down") }, WithHooks(Hooks{
		OnRetry: func(RetryEvent) { cancel() },
	}), WithRandomSource(func() float64 { return .5 }))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v, want context canceled", err)
	}
}

func TestDoPassesCalculatedDelayToWaiter(t *testing.T) {
	budget, _ := NewBudget(Config{Capacity: 1})
	policy := Policy{MaxAttempts: 2, BaseDelay: time.Second, MaxDelay: time.Second, Multiplier: 2, Jitter: true}
	var waited time.Duration
	calls := 0
	result, err := Do(context.Background(), budget, policy, func(context.Context) (int, error) {
		calls++
		if calls == 1 {
			return 0, errors.New("temporary")
		}
		return 7, nil
	}, WithRandomSource(func() float64 { return .25 }), withWaitFunction(func(_ context.Context, delay time.Duration) error {
		waited = delay
		return nil
	}))
	if err != nil || result != 7 || waited != 250*time.Millisecond {
		t.Fatalf("result=%d err=%v waited=%v", result, err, waited)
	}
}

func TestDoPermanentAndClassifier(t *testing.T) {
	budget, _ := NewBudget(Config{Capacity: 2})
	var calls atomic.Int32
	original := errors.New("bad input")
	err := DoVoid(context.Background(), budget, testPolicy(3), func(context.Context) error {
		calls.Add(1)
		return Permanent(original)
	})
	if calls.Load() != 1 || !errors.Is(err, original) || budget.Remaining() != 2 {
		t.Fatalf("calls=%d err=%v remaining=%d", calls.Load(), err, budget.Remaining())
	}

	calls.Store(0)
	err = DoVoid(context.Background(), budget, testPolicy(3), func(context.Context) error { calls.Add(1); return original }, WithClassifier(NeverRetry))
	if calls.Load() != 1 || !errors.Is(err, original) || budget.Remaining() != 2 {
		t.Fatalf("classifier calls=%d err=%v remaining=%d", calls.Load(), err, budget.Remaining())
	}
}

func TestDoDoesNotRetryContextErrors(t *testing.T) {
	budget, _ := NewBudget(Config{Capacity: 2})
	var calls int
	err := DoVoid(context.Background(), budget, testPolicy(3), func(context.Context) error {
		calls++
		return context.Canceled
	})
	if !errors.Is(err, context.Canceled) || calls != 1 || budget.Remaining() != 2 {
		t.Fatalf("err=%v calls=%d remaining=%d", err, calls, budget.Remaining())
	}
}

func TestDoHooks(t *testing.T) {
	budget, _ := NewBudget(Config{Capacity: 1})
	var attempts, failures, retries, exhausted int
	err := DoVoid(context.Background(), budget, testPolicy(3), func(context.Context) error { return errors.New("down") }, WithHooks(Hooks{
		OnAttempt:         func(AttemptEvent) { attempts++ },
		OnFailure:         func(FailureEvent) { failures++ },
		OnRetry:           func(RetryEvent) { retries++ },
		OnBudgetExhausted: func(BudgetEvent) { exhausted++ },
	}))
	if !errors.Is(err, ErrBudgetExhausted) || attempts != 2 || failures != 2 || retries != 1 || exhausted != 1 {
		t.Fatalf("err=%v attempts=%d failures=%d retries=%d exhausted=%d", err, attempts, failures, retries, exhausted)
	}

	budget.Reset()
	var successes int
	err = DoVoid(context.Background(), budget, testPolicy(1), func(context.Context) error { return nil }, WithHooks(Hooks{OnSuccess: func(SuccessEvent) { successes++ }}))
	if err != nil || successes != 1 {
		t.Fatalf("success err=%v hooks=%d", err, successes)
	}
}

func TestDoNilHooksAndInvalidInputs(t *testing.T) {
	budget, _ := NewBudget(Config{Capacity: 1})
	if err := DoVoid(context.Background(), budget, testPolicy(1), func(context.Context) error { return nil }, WithHooks(Hooks{})); err != nil {
		t.Fatal(err)
	}
	if _, err := Do[struct{}](context.Background(), nil, testPolicy(1), func(context.Context) (struct{}, error) { return struct{}{}, nil }); !errors.Is(err, ErrInvalidBudget) {
		t.Fatalf("nil budget err=%v", err)
	}
	if _, err := Do(context.Background(), budget, Policy{}, func(context.Context) (int, error) { return 0, nil }); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("invalid policy err=%v", err)
	}
	if _, err := Do[int](nil, budget, testPolicy(1), func(context.Context) (int, error) { return 0, nil }); err == nil {
		t.Fatal("nil context succeeded")
	}
	if _, err := Do[int](context.Background(), budget, testPolicy(1), nil); err == nil {
		t.Fatal("nil operation succeeded")
	}
	if _, err := Do(context.Background(), budget, testPolicy(1), func(context.Context) (int, error) { return 0, nil }, WithRandomSource(nil)); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("nil random source err=%v", err)
	}
	if err := DoVoid(context.Background(), budget, testPolicy(1), nil); err == nil {
		t.Fatal("nil void operation succeeded")
	}
}

type nilBudget struct{}

func (*nilBudget) Allow() bool      { panic("called typed-nil budget") }
func (*nilBudget) Remaining() int64 { panic("called typed-nil budget") }
func (*nilBudget) Capacity() int64  { panic("called typed-nil budget") }
func (*nilBudget) Reset()           { panic("called typed-nil budget") }

func TestDoRejectsTypedNilBudget(t *testing.T) {
	var budget *nilBudget
	called := false
	_, err := Do(context.Background(), budget, testPolicy(2), func(context.Context) (int, error) {
		called = true
		return 1, nil
	})
	if !errors.Is(err, ErrInvalidBudget) || called {
		t.Fatalf("err=%v called=%v", err, called)
	}
}
