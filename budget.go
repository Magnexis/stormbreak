package stormbreak

import (
	"fmt"
	"sync"
	"time"
)

// Budget controls whether a retry may proceed.
type Budget interface {
	// Allow consumes one retry token when capacity is available.
	Allow() bool
	// Remaining returns the currently available retry tokens.
	Remaining() int64
	// Capacity returns the maximum retry tokens.
	Capacity() int64
	// Reset restores the budget to full capacity.
	Reset()
}

// Config configures a token-based retry budget.
type Config struct {
	// Capacity is the maximum and initial token count.
	Capacity int64
	// RefillRate is the number of tokens restored per refill interval.
	RefillRate int64
	// RefillInterval controls how frequently elapsed time restores tokens.
	RefillInterval time.Duration
}

// BudgetSnapshot is an immutable view of a TokenBudget.
type BudgetSnapshot struct {
	Capacity       int64
	Remaining      int64
	RefillRate     int64
	RefillInterval time.Duration
	Exhausted      bool
}

// TokenBudget is a concurrency-safe, lazily refilled retry budget.
type TokenBudget struct {
	mu             sync.Mutex
	capacity       int64
	remaining      int64
	refillRate     int64
	refillInterval time.Duration
	lastRefill     time.Time
	now            func() time.Time
}

var _ Budget = (*TokenBudget)(nil)

// NewBudget validates config and creates a full retry budget.
func NewBudget(config Config) (*TokenBudget, error) { return newTokenBudget(config, time.Now) }

func newTokenBudget(config Config, now func() time.Time) (*TokenBudget, error) {
	if config.Capacity <= 0 {
		return nil, fmt.Errorf("%w: capacity must be greater than zero", ErrInvalidBudget)
	}
	if config.RefillRate < 0 {
		return nil, fmt.Errorf("%w: refill rate cannot be negative", ErrInvalidBudget)
	}
	if config.RefillRate > 0 && config.RefillInterval <= 0 {
		return nil, fmt.Errorf("%w: refill interval must be greater than zero when refill rate is positive", ErrInvalidBudget)
	}
	if config.RefillRate == 0 && config.RefillInterval < 0 {
		return nil, fmt.Errorf("%w: refill interval cannot be negative", ErrInvalidBudget)
	}
	if now == nil {
		return nil, fmt.Errorf("%w: clock cannot be nil", ErrInvalidBudget)
	}
	current := now()
	return &TokenBudget{capacity: config.Capacity, remaining: config.Capacity, refillRate: config.RefillRate, refillInterval: config.RefillInterval, lastRefill: current, now: now}, nil
}

// Allow consumes one retry token when capacity is available.
func (b *TokenBudget) Allow() bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refillLocked(b.now())
	if b.remaining == 0 {
		return false
	}
	b.remaining--
	return true
}

// Remaining returns the current token count after applying lazy refill.
func (b *TokenBudget) Remaining() int64 {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refillLocked(b.now())
	return b.remaining
}

// Capacity returns the maximum token count.
func (b *TokenBudget) Capacity() int64 {
	if b == nil {
		return 0
	}
	return b.capacity
}

// Reset restores full capacity and restarts the refill window.
func (b *TokenBudget) Reset() {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.remaining = b.capacity
	b.lastRefill = b.now()
	b.mu.Unlock()
}

// Snapshot returns an immutable view after applying lazy refill.
func (b *TokenBudget) Snapshot() BudgetSnapshot {
	if b == nil {
		return BudgetSnapshot{Exhausted: true}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refillLocked(b.now())
	return BudgetSnapshot{Capacity: b.capacity, Remaining: b.remaining, RefillRate: b.refillRate, RefillInterval: b.refillInterval, Exhausted: b.remaining == 0}
}

func (b *TokenBudget) refillLocked(now time.Time) {
	if b.refillRate == 0 || now.Before(b.lastRefill) {
		return
	}
	if b.remaining == b.capacity {
		// Time spent full cannot be banked for an immediate refill after use.
		b.lastRefill = now
		return
	}
	intervals := int64(now.Sub(b.lastRefill) / b.refillInterval)
	if intervals <= 0 {
		return
	}
	missing := b.capacity - b.remaining
	intervalsToFull := (missing-1)/b.refillRate + 1
	if intervals >= intervalsToFull {
		b.remaining = b.capacity
	} else {
		b.remaining += intervals * b.refillRate
	}
	b.lastRefill = b.lastRefill.Add(time.Duration(intervals) * b.refillInterval)
}
