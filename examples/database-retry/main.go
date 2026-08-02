package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/magnexis/stormbreak"
)

var errTemporaryDatabase = errors.New("temporary database failure")

type record struct {
	ID    string
	Value string
}

// repository stands in for a transactional data adapter. Assigning by stable ID
// makes this example operation safe to repeat; real databases need equivalent
// transaction and uniqueness guarantees.
type repository struct {
	mu       sync.Mutex
	records  map[string]record
	failOnce bool
}

func (r *repository) Save(_ context.Context, value record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failOnce {
		r.failOnce = false
		return errTemporaryDatabase
	}
	r.records[value.ID] = value
	return nil
}

func main() {
	databaseBudget, err := stormbreak.NewBudget(stormbreak.Config{Capacity: 10, RefillRate: 1, RefillInterval: time.Second})
	if err != nil {
		panic(err)
	}
	repository := &repository{records: make(map[string]record), failOnce: true}
	value := record{ID: "record-1", Value: "saved safely"}
	policy := stormbreak.Policy{MaxAttempts: 4, BaseDelay: 10 * time.Millisecond, MaxDelay: 100 * time.Millisecond, Multiplier: 2, Jitter: true}

	err = stormbreak.DoVoid(context.Background(), databaseBudget, policy, func(ctx context.Context) error {
		return repository.Save(ctx, value)
	}, stormbreak.WithClassifier(func(err error) bool {
		return errors.Is(err, errTemporaryDatabase)
	}))
	if err != nil {
		panic(err)
	}
	fmt.Println(repository.records[value.ID].Value)
}
