package main

import (
	"context"
	"fmt"
	"time"

	"github.com/magnexis/stormbreak"
)

func main() {
	budget, err := stormbreak.NewBudget(stormbreak.Config{Capacity: 100, RefillRate: 10, RefillInterval: time.Second})
	if err != nil {
		panic(err)
	}

	result, err := stormbreak.Do(context.Background(), budget, stormbreak.DefaultPolicy(), func(context.Context) (string, error) {
		return "resource", nil
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(result)
}
