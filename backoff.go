package stormbreak

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"time"
)

// Policy configures retry attempts and exponential backoff.
type Policy struct {
	// MaxAttempts includes the initial attempt.
	MaxAttempts int
	// BaseDelay is the delay cap before the first retry.
	BaseDelay time.Duration
	// MaxDelay caps exponential backoff.
	MaxDelay time.Duration
	// Multiplier controls exponential growth. Zero selects the default of two.
	Multiplier float64
	// Jitter randomizes each delay uniformly between zero and its calculated cap.
	Jitter bool
}

// DefaultPolicy returns conservative defaults suitable for network operations.
func DefaultPolicy() Policy {
	return Policy{MaxAttempts: 3, BaseDelay: 200 * time.Millisecond, MaxDelay: 5 * time.Second, Multiplier: 2, Jitter: true}
}

// Validate checks whether a policy can be executed safely.
func (p Policy) Validate() error {
	if p.MaxAttempts < 1 {
		return fmt.Errorf("%w: max attempts must be at least one", ErrInvalidPolicy)
	}
	if p.BaseDelay < 0 {
		return fmt.Errorf("%w: base delay cannot be negative", ErrInvalidPolicy)
	}
	if p.MaxDelay < 0 {
		return fmt.Errorf("%w: max delay cannot be negative", ErrInvalidPolicy)
	}
	if p.MaxDelay < p.BaseDelay {
		return fmt.Errorf("%w: max delay must be greater than or equal to base delay", ErrInvalidPolicy)
	}
	if math.IsNaN(p.Multiplier) || math.IsInf(p.Multiplier, 0) || p.Multiplier < 0 || (p.Multiplier > 0 && p.Multiplier < 1) {
		return fmt.Errorf("%w: multiplier must be zero (the default) or at least one", ErrInvalidPolicy)
	}
	return nil
}

// Backoff computes the delay before retry number attempt. Attempt one returns BaseDelay.
func Backoff(policy Policy, attempt int) time.Duration {
	return backoff(policy, attempt, rand.Float64)
}

func backoff(policy Policy, attempt int, random func() float64) time.Duration {
	if attempt < 1 || policy.BaseDelay <= 0 {
		return 0
	}
	delay := float64(policy.BaseDelay)
	multiplier := policy.Multiplier
	if multiplier == 0 {
		multiplier = 2
	}
	if attempt > 1 {
		delay *= math.Pow(multiplier, float64(attempt-1))
	}
	maxDelay := float64(policy.MaxDelay)
	if math.IsInf(delay, 0) || delay >= maxDelay {
		return jitterDuration(policy.MaxDelay, policy.Jitter, random)
	}
	return jitterDuration(time.Duration(delay), policy.Jitter, random)
}

func jitterDuration(delay time.Duration, enabled bool, random func() float64) time.Duration {
	if enabled && delay > 0 {
		value := random()
		if math.IsNaN(value) {
			// A broken source must not collapse backoff into an immediate retry.
			value = math.Nextafter(1, 0)
		} else if value < 0 {
			value = 0
		} else if value >= 1 {
			value = math.Nextafter(1, 0)
		}
		// Full jitter spreads retries uniformly from zero through the computed cap.
		return time.Duration(float64(delay) * value)
	}
	return delay
}

func wait(ctx context.Context, delay time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
