package stormbreak

import (
	"errors"
	"math"
	"testing"
	"time"
)

func TestPolicyValidation(t *testing.T) {
	invalid := []Policy{
		{},
		{MaxAttempts: 1, BaseDelay: -1, MaxDelay: 1, Multiplier: 1},
		{MaxAttempts: 1, BaseDelay: 2, MaxDelay: 1, Multiplier: 1},
		{MaxAttempts: 1, Multiplier: .5},
		{MaxAttempts: 1, Multiplier: -1},
		{MaxAttempts: 1, Multiplier: math.NaN()},
	}
	for _, policy := range invalid {
		if err := policy.Validate(); !errors.Is(err, ErrInvalidPolicy) {
			t.Fatalf("Validate(%+v) = %v", policy, err)
		}
	}
}

func TestBackoffUsesDefaultMultiplier(t *testing.T) {
	policy := Policy{MaxAttempts: 3, BaseDelay: time.Second, MaxDelay: 10 * time.Second}
	if err := policy.Validate(); err != nil {
		t.Fatal(err)
	}
	if got := Backoff(policy, 2); got != 2*time.Second {
		t.Fatalf("Backoff with default multiplier = %v, want 2s", got)
	}
}

func TestBackoffCalculationAndMaximum(t *testing.T) {
	policy := Policy{MaxAttempts: 10, BaseDelay: 100 * time.Millisecond, MaxDelay: 450 * time.Millisecond, Multiplier: 2}
	want := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond, 450 * time.Millisecond}
	for i, expected := range want {
		if got := Backoff(policy, i+1); got != expected {
			t.Fatalf("Backoff attempt %d = %v, want %v", i+1, got, expected)
		}
	}
}

func TestBackoffJitterBoundaries(t *testing.T) {
	policy := Policy{MaxAttempts: 2, BaseDelay: time.Second, MaxDelay: time.Second, Multiplier: 2, Jitter: true}
	if got := backoff(policy, 1, func() float64 { return 0 }); got != 0 {
		t.Fatalf("zero jitter = %v", got)
	}
	if got := backoff(policy, 1, func() float64 { return 1 }); got >= time.Second || got < 999*time.Millisecond {
		t.Fatalf("upper jitter = %v, want just below 1s", got)
	}
}

func TestBackoffHandlesInvalidRandomValues(t *testing.T) {
	policy := Policy{MaxAttempts: 2, BaseDelay: time.Second, MaxDelay: time.Second, Multiplier: 2, Jitter: true}
	if got := backoff(policy, 1, func() float64 { return math.NaN() }); got <= 0 || got >= time.Second {
		t.Fatalf("NaN jitter = %v, want a safe positive delay below cap", got)
	}
	if got := backoff(policy, 1, func() float64 { return math.Inf(1) }); got <= 0 || got >= time.Second {
		t.Fatalf("infinite jitter = %v, want a safe positive delay below cap", got)
	}
}

func TestBackoffDurationOverflow(t *testing.T) {
	policy := Policy{MaxAttempts: math.MaxInt, BaseDelay: time.Second, MaxDelay: time.Duration(math.MaxInt64), Multiplier: math.MaxFloat64}
	if got := Backoff(policy, math.MaxInt); got != time.Duration(math.MaxInt64) {
		t.Fatalf("Backoff overflow = %v, want max duration", got)
	}
}
