package httpretry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"
)

type timeoutError struct{}

func (timeoutError) Error() string   { return "timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return false }

func TestDefaultClassifierErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "EOF", err: io.EOF, want: true},
		{name: "wrapped EOF", err: fmt.Errorf("read response: %w", io.EOF), want: true},
		{name: "timeout", err: timeoutError{}, want: true},
		{name: "canceled", err: context.Canceled, want: false},
		{name: "deadline", err: context.DeadlineExceeded, want: false},
		{name: "ordinary", err: errors.New("ordinary"), want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := DefaultClassifier(nil, test.err); got != test.want {
				t.Fatalf("DefaultClassifier() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestDefaultClassifierStatuses(t *testing.T) {
	retryable := []int{408, 425, 429, 500, 502, 503, 504}
	for _, status := range retryable {
		if !DefaultClassifier(&http.Response{StatusCode: status}, nil) {
			t.Fatalf("status %d should be retryable", status)
		}
	}
	for _, status := range []int{200, 400, 401, 403, 404, 422} {
		if DefaultClassifier(&http.Response{StatusCode: status}, nil) {
			t.Fatalf("status %d should not be retryable", status)
		}
	}
	if DefaultClassifier(nil, nil) {
		t.Fatal("nil response and nil error must not be retryable")
	}
}

func TestRetrySafeRequestMethods(t *testing.T) {
	tests := []struct {
		method string
		key    bool
		want   bool
	}{
		{method: "", want: true},
		{method: http.MethodGet, want: true},
		{method: http.MethodPut, want: true},
		{method: http.MethodDelete, want: true},
		{method: http.MethodPost, want: false},
		{method: http.MethodPost, key: true, want: true},
		{method: http.MethodPatch, key: true, want: true},
		{method: http.MethodConnect, want: false},
	}
	for _, test := range tests {
		request := &http.Request{Method: test.method, Header: make(http.Header)}
		if test.key {
			request.Header.Set("Idempotency-Key", "request-1")
		}
		if got := retrySafeRequest(request); got != test.want {
			t.Fatalf("retrySafeRequest(%q, key=%v) = %v, want %v", test.method, test.key, got, test.want)
		}
	}
}
