package stormbreak

import (
	"math"
	"testing"
	"time"
)

func FuzzBackoffBounds(f *testing.F) {
	f.Add(1, int64(time.Millisecond), int64(time.Second), float64(2), float64(.5))
	f.Add(math.MaxInt, int64(time.Second), int64(math.MaxInt64), math.MaxFloat64, math.NaN())
	f.Fuzz(func(t *testing.T, attempt int, baseRaw, maxRaw int64, multiplier, randomValue float64) {
		policy := Policy{
			MaxAttempts: 1,
			BaseDelay:   time.Duration(baseRaw),
			MaxDelay:    time.Duration(maxRaw),
			Multiplier:  multiplier,
			Jitter:      true,
		}
		if policy.Validate() != nil {
			return
		}
		delay := backoff(policy, attempt, func() float64 { return randomValue })
		if delay < 0 || delay > policy.MaxDelay {
			t.Fatalf("backoff=%v outside [0,%v] for attempt=%d policy=%+v random=%v", delay, policy.MaxDelay, attempt, policy, randomValue)
		}
	})
}
