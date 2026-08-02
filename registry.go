package stormbreak

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Registry stores independently named retry budgets. It has no global instance.
type Registry struct {
	mu      sync.RWMutex
	budgets map[string]*TokenBudget
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry { return &Registry{budgets: make(map[string]*TokenBudget)} }

// Create validates and registers a new uniquely named budget.
func (r *Registry) Create(name string, config Config) (*TokenBudget, error) {
	if r == nil {
		return nil, fmt.Errorf("stormbreak: registry is nil")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("stormbreak: budget name cannot be empty")
	}
	budget, err := NewBudget(config)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.budgets[name]; exists {
		return nil, fmt.Errorf("stormbreak: budget %q already exists", name)
	}
	r.budgets[name] = budget
	return budget, nil
}

// Get retrieves a budget by its exact name.
func (r *Registry) Get(name string) (*TokenBudget, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	budget, ok := r.budgets[name]
	r.mu.RUnlock()
	return budget, ok
}

// Delete removes a named budget.
func (r *Registry) Delete(name string) bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.budgets[name]; !ok {
		return false
	}
	delete(r.budgets, name)
	return true
}

// Names returns sorted registered names.
func (r *Registry) Names() []string {
	if r == nil {
		return []string{}
	}
	r.mu.RLock()
	names := make([]string, 0, len(r.budgets))
	for name := range r.budgets {
		names = append(names, name)
	}
	r.mu.RUnlock()
	sort.Strings(names)
	return names
}

// Snapshot returns independent snapshots of all registered budgets.
func (r *Registry) Snapshot() map[string]BudgetSnapshot {
	result := make(map[string]BudgetSnapshot)
	if r == nil {
		return result
	}
	r.mu.RLock()
	budgets := make(map[string]*TokenBudget, len(r.budgets))
	for name, budget := range r.budgets {
		budgets[name] = budget
	}
	r.mu.RUnlock()
	for name, budget := range budgets {
		result[name] = budget.Snapshot()
	}
	return result
}
