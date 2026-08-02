package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/magnexis/stormbreak"
)

type message struct{ ID int }

func process(context.Context, message) error { return nil }

func main() {
	queueBudget, err := stormbreak.NewBudget(stormbreak.Config{Capacity: 50, RefillRate: 5, RefillInterval: time.Second})
	if err != nil {
		panic(err)
	}
	workerPolicy := stormbreak.Policy{MaxAttempts: 4, BaseDelay: 100 * time.Millisecond, MaxDelay: time.Second, Multiplier: 2, Jitter: true}
	messages := make(chan message, 10)

	var workers sync.WaitGroup
	for range 4 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for item := range messages {
				err := stormbreak.DoVoid(context.Background(), queueBudget, workerPolicy, func(ctx context.Context) error {
					return process(ctx, item)
				})
				if err != nil {
					fmt.Printf("message %d failed: %v\n", item.ID, err)
				}
			}
		}()
	}
	for id := range 10 {
		messages <- message{ID: id}
	}
	close(messages)
	workers.Wait()
}
