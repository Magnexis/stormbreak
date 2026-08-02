package stormbreak

import (
	"errors"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewBudgetValidation(t *testing.T) {
	tests := []Config{
		{},
		{Capacity: -1},
		{Capacity: 1, RefillRate: -1},
		{Capacity: 1, RefillRate: 1},
		{Capacity: 1, RefillInterval: -time.Second},
	}
	for _, config := range tests {
		if _, err := NewBudget(config); !errors.Is(err, ErrInvalidBudget) {
			t.Fatalf("NewBudget(%+v) error = %v, want ErrInvalidBudget", config, err)
		}
	}
}

func TestBudgetCapacityAndReset(t *testing.T) {
	budget, err := NewBudget(Config{Capacity: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !budget.Allow() || !budget.Allow() || budget.Allow() {
		t.Fatal("budget did not enforce capacity")
	}
	if got := budget.Remaining(); got != 0 {
		t.Fatalf("Remaining() = %d, want 0", got)
	}
	budget.Reset()
	if got := budget.Remaining(); got != 2 {
		t.Fatalf("Remaining() after Reset = %d, want 2", got)
	}
}

func TestBudgetLazyRefill(t *testing.T) {
	now := time.Unix(100, 0)
	budget, err := newTokenBudget(Config{Capacity: 5, RefillRate: 2, RefillInterval: time.Second}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	for range 5 {
		if !budget.Allow() {
			t.Fatal("unexpected exhaustion")
		}
	}
	now = now.Add(2500 * time.Millisecond)
	if got := budget.Remaining(); got != 4 {
		t.Fatalf("Remaining() = %d, want 4", got)
	}
	now = now.Add(500 * time.Millisecond)
	if got := budget.Remaining(); got != 5 {
		t.Fatalf("Remaining() = %d, want capped 5", got)
	}
}

func TestBudgetDoesNotBankRefillsWhileFull(t *testing.T) {
	now := time.Unix(100, 0)
	budget, err := newTokenBudget(Config{Capacity: 1, RefillRate: 1, RefillInterval: time.Second}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Hour)
	if !budget.Allow() {
		t.Fatal("first token unavailable")
	}
	if budget.Allow() {
		t.Fatal("time spent at full capacity was incorrectly banked")
	}
	now = now.Add(time.Second)
	if !budget.Allow() {
		t.Fatal("token did not refill after a complete interval")
	}
}

func TestBudgetIgnoresBackwardClockMovement(t *testing.T) {
	now := time.Unix(100, 0)
	budget, err := newTokenBudget(Config{Capacity: 2, RefillRate: 1, RefillInterval: time.Second}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	budget.Allow()
	now = now.Add(-time.Hour)
	if got := budget.Remaining(); got != 1 {
		t.Fatalf("Remaining() after backward clock = %d, want 1", got)
	}
	now = time.Unix(101, 0)
	if got := budget.Remaining(); got != 2 {
		t.Fatalf("Remaining() after clock recovery = %d, want 2", got)
	}
}

func TestBudgetRefillArithmeticDoesNotOverflow(t *testing.T) {
	now := time.Unix(100, 0)
	budget, err := newTokenBudget(Config{Capacity: math.MaxInt64, RefillRate: math.MaxInt64, RefillInterval: time.Second}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	budget.Allow()
	now = now.Add(time.Second)
	if got := budget.Remaining(); got != math.MaxInt64 {
		t.Fatalf("Remaining() = %d, want MaxInt64", got)
	}
}

func TestBudgetConcurrentAccess(t *testing.T) {
	budget, err := NewBudget(Config{Capacity: 1_000})
	if err != nil {
		t.Fatal(err)
	}
	var allowed atomic.Int64
	var group sync.WaitGroup
	for range 20 {
		group.Add(1)
		go func() {
			defer group.Done()
			for range 100 {
				if budget.Allow() {
					allowed.Add(1)
				}
			}
		}()
	}
	group.Wait()
	if got := allowed.Load(); got != 1_000 {
		t.Fatalf("allowed = %d, want 1000", got)
	}
}

func TestBudgetSnapshot(t *testing.T) {
	budget, _ := NewBudget(Config{Capacity: 1, RefillRate: 1, RefillInterval: time.Minute})
	budget.Allow()
	snapshot := budget.Snapshot()
	if !snapshot.Exhausted || snapshot.Remaining != 0 || snapshot.Capacity != 1 {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
}

func TestNilBudgetMethodsAreSafe(t *testing.T) {
	var budget *TokenBudget
	if budget.Allow() || budget.Remaining() != 0 || budget.Capacity() != 0 || !budget.Snapshot().Exhausted {
		t.Fatal("nil budget returned unexpected state")
	}
	budget.Reset()
}
