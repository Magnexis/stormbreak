package stormbreak_test

import (
	"context"
	"errors"
	"fmt"

	"github.com/magnexis/stormbreak"
)

func ExampleDo() {
	budget, _ := stormbreak.NewBudget(stormbreak.Config{Capacity: 1})
	policy := stormbreak.Policy{MaxAttempts: 2, Multiplier: 1}
	attempts := 0

	value, err := stormbreak.Do(context.Background(), budget, policy, func(context.Context) (int, error) {
		attempts++
		if attempts == 1 {
			return 0, errors.New("temporary failure")
		}
		return 42, nil
	})

	fmt.Println(value, err, attempts, budget.Remaining())
	// Output: 42 <nil> 2 0
}

func ExamplePermanent() {
	budget, _ := stormbreak.NewBudget(stormbreak.Config{Capacity: 5})
	attempts := 0
	err := stormbreak.DoVoid(context.Background(), budget, stormbreak.DefaultPolicy(), func(context.Context) error {
		attempts++
		return stormbreak.Permanent(errors.New("invalid credentials"))
	})

	fmt.Println(stormbreak.IsPermanent(err), attempts, budget.Remaining())
	// Output: true 1 5
}

func ExampleRegistry() {
	registry := stormbreak.NewRegistry()
	_, _ = registry.Create("database", stormbreak.Config{Capacity: 10})
	_, _ = registry.Create("github-api", stormbreak.Config{Capacity: 20})

	fmt.Println(registry.Names())
	// Output: [database github-api]
}

func ExampleWithHooks() {
	budget, _ := stormbreak.NewBudget(stormbreak.Config{Capacity: 1})
	policy := stormbreak.Policy{MaxAttempts: 2, Multiplier: 1}
	attempts := 0
	hooks := stormbreak.Hooks{
		OnRetry: func(event stormbreak.RetryEvent) {
			fmt.Printf("retry attempt=%d remaining=%d\n", event.Attempt, event.BudgetRemaining)
		},
	}

	_ = stormbreak.DoVoid(context.Background(), budget, policy, func(context.Context) error {
		attempts++
		if attempts == 1 {
			return errors.New("temporary")
		}
		return nil
	}, stormbreak.WithHooks(hooks))
	// Output: retry attempt=2 remaining=0
}
