package httpretry

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/magnexis/stormbreak"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

type trackedBody struct {
	io.Reader
	closed atomic.Bool
}

func (b *trackedBody) Close() error { b.closed.Store(true); return nil }

type temporaryError struct{}

func (temporaryError) Error() string   { return "temporary network failure" }
func (temporaryError) Timeout() bool   { return false }
func (temporaryError) Temporary() bool { return true }

func httpPolicy(attempts int) stormbreak.Policy {
	return stormbreak.Policy{MaxAttempts: attempts, BaseDelay: 0, MaxDelay: 0, Multiplier: 1}
}

func httpBudget(t *testing.T, capacity int64) *stormbreak.TokenBudget {
	t.Helper()
	budget, err := stormbreak.NewBudget(stormbreak.Config{Capacity: capacity})
	if err != nil {
		t.Fatal(err)
	}
	return budget
}

func TestTransportRetriesResponseAndClosesBody(t *testing.T) {
	var calls int
	firstBody := &trackedBody{Reader: strings.NewReader("unavailable")}
	transport := &Transport{
		Budget: httpBudget(t, 2), Policy: httpPolicy(3),
		Base: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: make(http.Header), Body: firstBody, Request: request}, nil
			}
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("ok")), Request: request}, nil
		}),
	}
	request, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if calls != 2 || response.StatusCode != http.StatusOK || !firstBody.closed.Load() {
		t.Fatalf("calls=%d status=%d firstClosed=%v", calls, response.StatusCode, firstBody.closed.Load())
	}
}

func TestTransportHooks(t *testing.T) {
	var calls int
	var attemptEvents []int
	var failureEvent stormbreak.FailureEvent
	var retryEvent stormbreak.RetryEvent
	var successEvent stormbreak.SuccessEvent
	transport := &Transport{
		Budget: httpBudget(t, 1), Policy: httpPolicy(2),
		Hooks: stormbreak.Hooks{
			OnAttempt: func(event stormbreak.AttemptEvent) { attemptEvents = append(attemptEvents, event.Attempt) },
			OnFailure: func(event stormbreak.FailureEvent) { failureEvent = event },
			OnRetry:   func(event stormbreak.RetryEvent) { retryEvent = event },
			OnSuccess: func(event stormbreak.SuccessEvent) { successEvent = event },
		},
		Base: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			calls++
			status := http.StatusServiceUnavailable
			if calls == 2 {
				status = http.StatusOK
			}
			return &http.Response{StatusCode: status, Status: http.StatusText(status), Header: make(http.Header), Body: http.NoBody, Request: request}, nil
		}),
	}
	request, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
	response, err := transport.RoundTrip(request)
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("response=%v err=%v", response, err)
	}
	var statusErr *StatusError
	if len(attemptEvents) != 2 || attemptEvents[0] != 1 || attemptEvents[1] != 2 ||
		failureEvent.Attempt != 1 || !errors.As(failureEvent.Error, &statusErr) || statusErr.StatusCode != http.StatusServiceUnavailable ||
		retryEvent.Attempt != 2 || retryEvent.BudgetRemaining != 0 || retryEvent.Error != failureEvent.Error ||
		successEvent.Attempts != 2 {
		t.Fatalf("attempts=%v failure=%+v retry=%+v success=%+v", attemptEvents, failureEvent, retryEvent, successEvent)
	}
}

func TestTransportRetriesTemporaryNetworkFailure(t *testing.T) {
	var calls int
	transport := &Transport{
		Budget: httpBudget(t, 1), Policy: httpPolicy(2),
		Base: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				return nil, temporaryError{}
			}
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: http.NoBody, Request: request}, nil
		}),
	}
	request, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
	response, err := transport.RoundTrip(request)
	if err != nil || response.StatusCode != http.StatusOK || calls != 2 {
		t.Fatalf("response=%v err=%v calls=%d", response, err, calls)
	}
}

func TestTransportReplaysBody(t *testing.T) {
	var calls int
	var bodies []string
	transport := &Transport{
		Budget: httpBudget(t, 1), Policy: httpPolicy(2),
		Base: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			calls++
			body, _ := io.ReadAll(request.Body)
			_ = request.Body.Close()
			bodies = append(bodies, string(body))
			status := http.StatusServiceUnavailable
			if calls == 2 {
				status = http.StatusOK
			}
			return &http.Response{StatusCode: status, Header: make(http.Header), Body: http.NoBody, Request: request}, nil
		}),
	}
	request, _ := http.NewRequest(http.MethodPost, "https://example.test", bytes.NewBufferString("payload"))
	request.Header.Set("Idempotency-Key", "job-123")
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || len(bodies) != 2 || bodies[0] != "payload" || bodies[1] != "payload" {
		t.Fatalf("status=%d bodies=%v", response.StatusCode, bodies)
	}
}

func TestTransportDoesNotRetryNonReplayableBody(t *testing.T) {
	var calls int
	transport := &Transport{
		Budget: httpBudget(t, 1), Policy: httpPolicy(2),
		Base: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: make(http.Header), Body: http.NoBody, Request: request}, nil
		}),
	}
	request, _ := http.NewRequest(http.MethodPost, "https://example.test", io.NopCloser(strings.NewReader("payload")))
	request.Header.Set("Idempotency-Key", "job-123")
	response, err := transport.RoundTrip(request)
	if err != nil || response.StatusCode != http.StatusServiceUnavailable || calls != 1 {
		t.Fatalf("status=%d err=%v calls=%d", response.StatusCode, err, calls)
	}
}

func TestTransportDefaultClassifierDoesNotRetryAuthenticationFailure(t *testing.T) {
	var calls int
	transport := &Transport{
		Budget: httpBudget(t, 1), Policy: httpPolicy(2),
		Base: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: http.StatusUnauthorized, Header: make(http.Header), Body: http.NoBody, Request: request}, nil
		}),
	}
	request, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
	response, err := transport.RoundTrip(request)
	if err != nil || response.StatusCode != http.StatusUnauthorized || calls != 1 {
		t.Fatalf("status=%d err=%v calls=%d", response.StatusCode, err, calls)
	}
}

func TestTransportBudgetExhaustion(t *testing.T) {
	budget := httpBudget(t, 1)
	budget.Allow()
	body := &trackedBody{Reader: strings.NewReader("busy")}
	var budgetEvent stormbreak.BudgetEvent
	transport := &Transport{
		Budget: budget, Policy: httpPolicy(2),
		Hooks: stormbreak.Hooks{OnBudgetExhausted: func(event stormbreak.BudgetEvent) { budgetEvent = event }},
		Base: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusTooManyRequests, Header: make(http.Header), Body: body, Request: request}, nil
		}),
	}
	request, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
	response, err := transport.RoundTrip(request)
	var statusErr *StatusError
	if response != nil || !errors.Is(err, stormbreak.ErrBudgetExhausted) || !body.closed.Load() ||
		budgetEvent.Attempts != 1 || budgetEvent.Remaining != 0 || !errors.As(budgetEvent.LastError, &statusErr) || statusErr.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("response=%v err=%v closed=%v event=%+v", response, err, body.closed.Load(), budgetEvent)
	}
}

func TestTransportNonRetryableResponseEmitsSuccess(t *testing.T) {
	var successes, failures int
	transport := &Transport{
		Budget: httpBudget(t, 1), Policy: httpPolicy(2),
		Hooks: stormbreak.Hooks{
			OnSuccess: func(stormbreak.SuccessEvent) { successes++ },
			OnFailure: func(stormbreak.FailureEvent) { failures++ },
		},
		Base: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusUnauthorized, Header: make(http.Header), Body: http.NoBody, Request: request}, nil
		}),
	}
	request, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
	_, err := transport.RoundTrip(request)
	if err != nil || successes != 1 || failures != 0 {
		t.Fatalf("err=%v successes=%d failures=%d", err, successes, failures)
	}
}

func TestRetryAfterDelay(t *testing.T) {
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	response := &http.Response{Header: http.Header{"Retry-After": []string{"3"}}}
	if got := retryAfterDelay(response, now); got != 3*time.Second {
		t.Fatalf("seconds delay=%v", got)
	}
	response.Header.Set("Retry-After", now.Add(5*time.Second).Format(http.TimeFormat))
	if got := retryAfterDelay(response, now); got != 5*time.Second {
		t.Fatalf("date delay=%v", got)
	}
	response.Header.Set("Retry-After", " 3 ")
	if got := retryAfterDelay(response, now); got != 3*time.Second {
		t.Fatalf("trimmed seconds delay=%v", got)
	}
}

func TestTransportUsesAndBoundsRetryAfter(t *testing.T) {
	fixedNow := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	var calls int
	var waited time.Duration
	transport := &Transport{
		Budget:        httpBudget(t, 1),
		Policy:        stormbreak.Policy{MaxAttempts: 2, BaseDelay: time.Second, MaxDelay: time.Second, Multiplier: 2},
		MaxRetryAfter: 2 * time.Second,
		now:           func() time.Time { return fixedNow },
		backoff:       func(stormbreak.Policy, int) time.Duration { return time.Second },
		wait: func(_ context.Context, delay time.Duration) error {
			waited = delay
			return nil
		},
		Base: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			calls++
			status := http.StatusTooManyRequests
			header := http.Header{"Retry-After": []string{"30"}}
			if calls == 2 {
				status = http.StatusOK
				header = make(http.Header)
			}
			return &http.Response{StatusCode: status, Header: header, Body: http.NoBody, Request: request}, nil
		}),
	}
	request, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
	response, err := transport.RoundTrip(request)
	if err != nil || response.StatusCode != http.StatusOK || calls != 2 || waited != 2*time.Second {
		t.Fatalf("response=%v err=%v calls=%d waited=%v", response, err, calls, waited)
	}
}

func TestTransportRejectsMalformedBaseResult(t *testing.T) {
	transport := &Transport{
		Budget: httpBudget(t, 1), Policy: httpPolicy(2),
		Base: roundTripperFunc(func(*http.Request) (*http.Response, error) { return nil, nil }),
	}
	request, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
	response, err := transport.RoundTrip(request)
	if response != nil || err == nil || !strings.Contains(err.Error(), "nil response and nil error") {
		t.Fatalf("response=%v err=%v", response, err)
	}
}

func TestTransportClosesResponseReturnedWithError(t *testing.T) {
	body := &trackedBody{Reader: strings.NewReader("partial")}
	transport := &Transport{
		Budget: httpBudget(t, 1), Policy: httpPolicy(1),
		Classifier: func(*http.Response, error) bool { return false },
		Base: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusBadGateway, Header: make(http.Header), Body: body, Request: request}, errors.New("transport failed")
		}),
	}
	request, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
	response, err := transport.RoundTrip(request)
	if response != nil || err == nil || !body.closed.Load() {
		t.Fatalf("response=%v err=%v closed=%v", response, err, body.closed.Load())
	}
}

func TestTransportDoesNotRetryPostWithoutIdempotencyKey(t *testing.T) {
	var calls int
	transport := &Transport{
		Budget: httpBudget(t, 1), Policy: httpPolicy(2),
		Base: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: make(http.Header), Body: http.NoBody, Request: request}, nil
		}),
	}
	request, _ := http.NewRequest(http.MethodPost, "https://example.test", nil)
	response, err := transport.RoundTrip(request)
	if err != nil || response.StatusCode != http.StatusServiceUnavailable || calls != 1 {
		t.Fatalf("response=%v err=%v calls=%d", response, err, calls)
	}
}

func TestTransportCancellationDuringWaitClosesResponse(t *testing.T) {
	body := &trackedBody{Reader: strings.NewReader("busy")}
	transport := &Transport{
		Budget: httpBudget(t, 1), Policy: httpPolicy(2),
		wait: func(context.Context, time.Duration) error { return context.Canceled },
		Base: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: make(http.Header), Body: body, Request: request}, nil
		}),
	}
	request, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
	response, err := transport.RoundTrip(request)
	if response != nil || !errors.Is(err, context.Canceled) || !body.closed.Load() {
		t.Fatalf("response=%v err=%v closed=%v", response, err, body.closed.Load())
	}
}

func TestTransportRejectsInvalidMaxRetryAfter(t *testing.T) {
	transport := &Transport{Budget: httpBudget(t, 1), Policy: httpPolicy(1), MaxRetryAfter: -time.Second}
	request, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
	_, err := transport.RoundTrip(request)
	if !errors.Is(err, stormbreak.ErrInvalidPolicy) || !strings.Contains(err.Error(), "max retry-after") {
		t.Fatalf("err=%v", err)
	}
}

func TestTransportAttemptsExhaustedForNetworkFailure(t *testing.T) {
	transport := &Transport{
		Budget: httpBudget(t, 2), Policy: httpPolicy(2),
		Base: roundTripperFunc(func(*http.Request) (*http.Response, error) { return nil, temporaryError{} }),
	}
	request, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
	response, err := transport.RoundTrip(request)
	var retryErr *stormbreak.RetryError
	if response != nil || !errors.Is(err, stormbreak.ErrAttemptsExhausted) || !errors.As(err, &retryErr) || retryErr.Attempts != 2 {
		t.Fatalf("response=%v err=%v", response, err)
	}
}

func TestTransportCustomClassifier(t *testing.T) {
	var calls int
	transport := &Transport{
		Budget: httpBudget(t, 1), Policy: httpPolicy(2),
		Classifier: func(response *http.Response, _ error) bool {
			return response != nil && response.StatusCode == http.StatusConflict
		},
		Base: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			calls++
			status := http.StatusConflict
			if calls == 2 {
				status = http.StatusOK
			}
			return &http.Response{StatusCode: status, Header: make(http.Header), Body: http.NoBody, Request: request}, nil
		}),
	}
	request, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
	response, err := transport.RoundTrip(request)
	if err != nil || response.StatusCode != http.StatusOK || calls != 2 {
		t.Fatalf("response=%v err=%v calls=%d", response, err, calls)
	}
}

func TestTransportGetBodyFailure(t *testing.T) {
	var calls int
	transport := &Transport{
		Budget: httpBudget(t, 1), Policy: httpPolicy(2),
		Base: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: make(http.Header), Body: http.NoBody, Request: request}, nil
		}),
	}
	request, _ := http.NewRequest(http.MethodPut, "https://example.test", strings.NewReader("payload"))
	request.GetBody = func() (io.ReadCloser, error) { return nil, errors.New("body source unavailable") }
	response, err := transport.RoundTrip(request)
	if response != nil || err == nil || !strings.Contains(err.Error(), "replay request body") || calls != 1 {
		t.Fatalf("response=%v err=%v calls=%d", response, err, calls)
	}
}

func TestRetryAfterDelayInvalidAndOverflow(t *testing.T) {
	now := time.Now()
	response := &http.Response{Header: make(http.Header)}
	for _, value := range []string{"invalid", "-1", "0", now.Add(-time.Second).Format(http.TimeFormat)} {
		response.Header.Set("Retry-After", value)
		if got := retryAfterDelay(response, now); got != 0 {
			t.Fatalf("Retry-After %q = %v, want zero", value, got)
		}
	}
	response.Header.Set("Retry-After", "999999999999999999")
	if got := retryAfterDelay(response, now); got != time.Duration(1<<63-1) {
		t.Fatalf("overflow Retry-After = %v, want max duration", got)
	}
}

func TestWaitForRetry(t *testing.T) {
	if err := waitForRetry(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitForRetry(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled wait error=%v", err)
	}
	started := time.Now()
	if err := waitForRetry(context.Background(), time.Millisecond); err != nil || time.Since(started) < time.Millisecond {
		t.Fatalf("timer wait err=%v elapsed=%v", err, time.Since(started))
	}
}

type typedNilBudget struct{}

func (*typedNilBudget) Allow() bool      { panic("called typed-nil budget") }
func (*typedNilBudget) Remaining() int64 { panic("called typed-nil budget") }
func (*typedNilBudget) Capacity() int64  { panic("called typed-nil budget") }
func (*typedNilBudget) Reset()           { panic("called typed-nil budget") }

func TestTransportRejectsTypedNilBudget(t *testing.T) {
	var budget *typedNilBudget
	transport := &Transport{Budget: budget, Policy: httpPolicy(1)}
	request, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
	_, err := transport.RoundTrip(request)
	if !errors.Is(err, stormbreak.ErrInvalidBudget) {
		t.Fatalf("err=%v", err)
	}
}

func TestTransportRespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	transport := &Transport{Budget: httpBudget(t, 1), Policy: httpPolicy(2), Base: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("base transport called")
		return nil, nil
	})}
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.test", nil)
	_, err := transport.RoundTrip(request)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}
