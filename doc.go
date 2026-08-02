// Package stormbreak coordinates retry pressure through shared, concurrency-safe
// token budgets. Operations run once without cost; every subsequent attempt must
// consume shared capacity before applying context-aware exponential backoff.
package stormbreak
