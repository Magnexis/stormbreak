package httpretry

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/magnexis/stormbreak"
)

// Transport retries safe HTTP requests while sharing retry capacity through Budget.
// POST and PATCH requests are retried only when they include an Idempotency-Key header.
type Transport struct {
	Base       http.RoundTripper
	Budget     stormbreak.Budget
	Policy     stormbreak.Policy
	Classifier func(*http.Response, error) bool
	// Hooks observes HTTP attempts using the same synchronous event model as stormbreak.Do.
	Hooks stormbreak.Hooks
	// MaxRetryAfter bounds server-requested delays. Zero uses one minute.
	MaxRetryAfter time.Duration

	// Timing functions are private test seams and remain immutable during use.
	now     func() time.Time
	wait    func(context.Context, time.Duration) error
	backoff func(stormbreak.Policy, int) time.Duration
}

const defaultMaxRetryAfter = time.Minute

var _ http.RoundTripper = (*Transport)(nil)

// RoundTrip implements http.RoundTripper.
func (t *Transport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil {
		return nil, fmt.Errorf("stormbreak/httpretry: request cannot be nil")
	}
	if t == nil || nilBudget(t.Budget) {
		return nil, fmt.Errorf("%w: HTTP transport budget cannot be nil", stormbreak.ErrInvalidBudget)
	}
	if err := t.Policy.Validate(); err != nil {
		return nil, err
	}
	if t.MaxRetryAfter < 0 {
		return nil, fmt.Errorf("%w: HTTP max retry-after cannot be negative", stormbreak.ErrInvalidPolicy)
	}
	if err := request.Context().Err(); err != nil {
		return nil, err
	}

	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	classifier := t.Classifier
	if classifier == nil {
		classifier = DefaultClassifier
	}
	now := t.now
	if now == nil {
		now = time.Now
	}
	waiter := t.wait
	if waiter == nil {
		waiter = waitForRetry
	}
	backoff := t.backoff
	if backoff == nil {
		backoff = stormbreak.Backoff
	}
	maxRetryAfter := t.MaxRetryAfter
	if maxRetryAfter == 0 {
		maxRetryAfter = defaultMaxRetryAfter
	}
	replayable := retrySafeRequest(request)
	started := time.Now()

	for attempt := 1; attempt <= t.Policy.MaxAttempts; attempt++ {
		if t.Hooks.OnAttempt != nil {
			t.Hooks.OnAttempt(stormbreak.AttemptEvent{Attempt: attempt})
		}
		attemptRequest, err := requestForAttempt(request, attempt)
		if err != nil {
			emitFailure(t.Hooks, attempt, err)
			return nil, err
		}
		response, roundTripErr := base.RoundTrip(attemptRequest)
		if ctxErr := request.Context().Err(); ctxErr != nil {
			if roundTripErr != nil {
				emitFailure(t.Hooks, attempt, roundTripErr)
			}
			closeResponse(response)
			return nil, ctxErr
		}
		if response == nil && roundTripErr == nil {
			err := fmt.Errorf("stormbreak/httpretry: base transport returned a nil response and nil error")
			emitFailure(t.Hooks, attempt, err)
			return nil, err
		}
		retryable := classifier(response, roundTripErr)
		attemptErr := roundTripErr
		if attemptErr == nil && retryable {
			attemptErr = statusError(response)
		}
		if attemptErr != nil {
			emitFailure(t.Hooks, attempt, attemptErr)
		}
		if !retryable || !replayable {
			if roundTripErr != nil && response != nil {
				closeResponse(response)
				response = nil
			}
			if !retryable && roundTripErr == nil && t.Hooks.OnSuccess != nil {
				t.Hooks.OnSuccess(stormbreak.SuccessEvent{Attempts: attempt, Elapsed: time.Since(started)})
			}
			return response, roundTripErr
		}
		if attempt == t.Policy.MaxAttempts {
			if response != nil && roundTripErr == nil {
				return response, nil
			}
			closeResponse(response)
			return nil, &stormbreak.RetryError{Attempts: attempt, LastError: roundTripErr, Cause: stormbreak.ErrAttemptsExhausted}
		}
		if !t.Budget.Allow() {
			lastError := attemptErr
			closeResponse(response)
			remaining := t.Budget.Remaining()
			if t.Hooks.OnBudgetExhausted != nil {
				t.Hooks.OnBudgetExhausted(stormbreak.BudgetEvent{Attempts: attempt, LastError: lastError, Remaining: remaining})
			}
			return nil, &stormbreak.RetryError{Attempts: attempt, LastError: lastError, Cause: stormbreak.ErrBudgetExhausted}
		}

		delay := backoff(t.Policy, attempt)
		retryAfter := retryAfterDelay(response, now())
		if retryAfter > maxRetryAfter {
			retryAfter = maxRetryAfter
		}
		if retryAfter > delay {
			delay = retryAfter
		}
		closeResponse(response)
		if t.Hooks.OnRetry != nil {
			t.Hooks.OnRetry(stormbreak.RetryEvent{Attempt: attempt + 1, Delay: delay, Error: attemptErr, BudgetRemaining: t.Budget.Remaining()})
		}
		if err := waiter(request.Context(), delay); err != nil {
			return nil, err
		}
	}
	panic("stormbreak/httpretry: unreachable retry state")
}

func emitFailure(hooks stormbreak.Hooks, attempt int, err error) {
	if hooks.OnFailure != nil {
		hooks.OnFailure(stormbreak.FailureEvent{Attempt: attempt, Error: err})
	}
}

func requestForAttempt(request *http.Request, attempt int) (*http.Request, error) {
	clone := request.Clone(request.Context())
	if attempt == 1 || request.Body == nil || request.Body == http.NoBody {
		return clone, nil
	}
	body, err := request.GetBody()
	if err != nil {
		return nil, fmt.Errorf("stormbreak/httpretry: replay request body: %w", err)
	}
	clone.Body = body
	return clone, nil
}

func retryAfterDelay(response *http.Response, now time.Time) time.Duration {
	if response == nil {
		return 0
	}
	value := strings.TrimSpace(response.Header.Get("Retry-After"))
	if value == "" {
		return 0
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds <= 0 {
			return 0
		}
		if seconds > int64((time.Duration(1<<63-1))/time.Second) {
			return time.Duration(1<<63 - 1)
		}
		return time.Duration(seconds) * time.Second
	}
	date, err := http.ParseTime(value)
	if err != nil || !date.After(now) {
		return 0
	}
	return date.Sub(now)
}

func nilBudget(budget stormbreak.Budget) bool {
	switch typed := budget.(type) {
	case nil:
		return true
	case *stormbreak.TokenBudget:
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

func closeResponse(response *http.Response) {
	if response == nil || response.Body == nil {
		return
	}
	_, _ = io.CopyN(io.Discard, response.Body, 4<<10)
	_ = response.Body.Close()
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
