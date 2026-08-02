package stormbreak

import (
	"sync"
	"testing"
)

func TestRegistryLifecycle(t *testing.T) {
	registry := NewRegistry()
	config := Config{Capacity: 2}
	budget, err := registry.Create("database", config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Create("database", config); err == nil {
		t.Fatal("duplicate Create succeeded")
	}
	got, ok := registry.Get("database")
	if !ok || got != budget {
		t.Fatal("Get did not return created budget")
	}
	if names := registry.Names(); len(names) != 1 || names[0] != "database" {
		t.Fatalf("Names() = %v", names)
	}
	if snapshot := registry.Snapshot()["database"]; snapshot.Capacity != 2 {
		t.Fatalf("Snapshot() = %+v", snapshot)
	}
	if !registry.Delete("database") || registry.Delete("database") {
		t.Fatal("Delete result mismatch")
	}
}

func TestRegistrySnapshotsAreDetachedAndNamesSorted(t *testing.T) {
	registry := NewRegistry()
	first, _ := registry.Create("zeta", Config{Capacity: 2})
	_, _ = registry.Create("alpha", Config{Capacity: 1})
	first.Allow()

	names := registry.Names()
	if len(names) != 2 || names[0] != "alpha" || names[1] != "zeta" {
		t.Fatalf("Names() = %v", names)
	}
	snapshot := registry.Snapshot()
	snapshot["zeta"] = BudgetSnapshot{Remaining: 99}
	if got := first.Remaining(); got != 1 {
		t.Fatalf("mutating snapshot changed budget, remaining=%d", got)
	}
}

func TestNilRegistryIsSafe(t *testing.T) {
	var registry *Registry
	if _, ok := registry.Get("missing"); ok || registry.Delete("missing") || len(registry.Names()) != 0 || len(registry.Snapshot()) != 0 {
		t.Fatal("nil registry read methods returned unexpected data")
	}
	if _, err := registry.Create("name", Config{Capacity: 1}); err == nil {
		t.Fatal("nil registry Create succeeded")
	}
}

func TestRegistryConcurrentAccess(t *testing.T) {
	registry := NewRegistry()
	var group sync.WaitGroup
	for i := range 100 {
		group.Add(1)
		go func() {
			defer group.Done()
			name := string(rune('a' + i))
			_, _ = registry.Create(name, Config{Capacity: 1})
			registry.Get(name)
			registry.Snapshot()
		}()
	}
	group.Wait()
}
