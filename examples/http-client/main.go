package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/magnexis/stormbreak"
	"github.com/magnexis/stormbreak/httpretry"
)

func main() {
	budget, err := stormbreak.NewBudget(stormbreak.Config{Capacity: 20, RefillRate: 2, RefillInterval: time.Second})
	if err != nil {
		panic(err)
	}
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &httpretry.Transport{
			Budget: budget,
			Policy: stormbreak.DefaultPolicy(),
			Hooks: stormbreak.Hooks{
				OnRetry: func(event stormbreak.RetryEvent) {
					fmt.Printf("retrying HTTP request: attempt=%d delay=%s remaining=%d\n", event.Attempt, event.Delay, event.BudgetRemaining)
				},
			},
		},
	}

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com", nil)
	if err != nil {
		panic(err)
	}
	response, err := client.Do(request)
	if err != nil {
		panic(err)
	}
	defer response.Body.Close()
	fmt.Println(response.Status)
}
