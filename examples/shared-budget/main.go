package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/magnexis/stormbreak"
)

func main() {
	budget, err := stormbreak.NewBudget(stormbreak.Config{Capacity: 3, RefillRate: 1, RefillInterval: time.Second})
	if err != nil {
		panic(err)
	}
	policy := stormbreak.Policy{MaxAttempts: 3, BaseDelay: 50 * time.Millisecond, MaxDelay: 200 * time.Millisecond, Multiplier: 2, Jitter: true}

	var workers sync.WaitGroup
	for id := range 5 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			err := stormbreak.DoVoid(context.Background(), budget, policy, func(context.Context) error {
				return errors.New("shared dependency unavailable")
			})
			fmt.Printf("worker %d: %v\n", id, err)
		}()
	}
	workers.Wait()
}
