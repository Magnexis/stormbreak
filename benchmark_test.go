package stormbreak

import (
	"context"
	"errors"
	"testing"
)

func BenchmarkBudgetAllow(b *testing.B) {
	budget, _ := NewBudget(Config{Capacity: int64(b.N)})
	b.ResetTimer()
	for range b.N {
		budget.Allow()
	}
}

func BenchmarkBudgetAllowParallel(b *testing.B) {
	budget, _ := NewBudget(Config{Capacity: int64(b.N)})
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			budget.Allow()
		}
	})
}

func BenchmarkDoSuccess(b *testing.B) {
	budget, _ := NewBudget(Config{Capacity: 1})
	policy := testPolicy(1)
	b.ResetTimer()
	for range b.N {
		_, _ = Do(context.Background(), budget, policy, func(context.Context) (int, error) { return 1, nil })
	}
}

func BenchmarkDoSingleRetry(b *testing.B) {
	budget, _ := NewBudget(Config{Capacity: int64(b.N)})
	policy := testPolicy(2)
	b.ResetTimer()
	for range b.N {
		attempt := 0
		_, _ = Do(context.Background(), budget, policy, func(context.Context) (int, error) {
			attempt++
			if attempt == 1 {
				return 0, errors.New("retry")
			}
			return 1, nil
		})
	}
}

func BenchmarkRegistryGet(b *testing.B) {
	registry := NewRegistry()
	_, _ = registry.Create("service", Config{Capacity: 1})
	b.ResetTimer()
	for range b.N {
		registry.Get("service")
	}
}
