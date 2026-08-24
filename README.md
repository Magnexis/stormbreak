<div align="center">

<a href="https://pkg.go.dev/github.com/magnexis/stormbreak">
  <img src="assets/stormbreak-logo.png" alt="Stormbreak logo" width="220">
</a>

# STORMBREAK

**Shared retry budgets for resilient Go applications.**

[![Release](https://img.shields.io/github/v/release/Magnexis/stormbreak?sort=semver)](https://github.com/Magnexis/stormbreak/releases)
[![CI](https://github.com/Magnexis/stormbreak/actions/workflows/ci.yml/badge.svg)](https://github.com/Magnexis/stormbreak/actions/workflows/ci.yml)
[![CodeQL](https://github.com/Magnexis/stormbreak/actions/workflows/codeql.yml/badge.svg)](https://github.com/Magnexis/stormbreak/actions/workflows/codeql.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/magnexis/stormbreak.svg)](https://pkg.go.dev/github.com/magnexis/stormbreak)
[![Go Version](https://img.shields.io/github/go-mod/go-version/Magnexis/stormbreak)](go.mod)
[![Dependencies](https://img.shields.io/badge/dependencies-standard%20library-2f81f7)](go.mod)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

**Official Go module:**
[`github.com/magnexis/stormbreak`](https://pkg.go.dev/github.com/magnexis/stormbreak)

</div>

Stormbreak prevents failing services from being overwhelmed by the retries
intended to save them.

It is a small, standard-library-only Go module for coordinating retry pressure
across HTTP clients, database operations, message consumers, external APIs,
background workers, and other failure-prone operations.

> **Current release:** [`v0.1.0`](https://github.com/Magnexis/stormbreak/releases/tag/v0.1.0).
> Stormbreak is usable today, but remains pre-1.0. Review the
> [changelog](CHANGELOG.md) before upgrading minor versions.

## Contents

- [Why retry budgets matter](#why-retry-budgets-matter)
- [How Stormbreak works](#how-stormbreak-works)
- [Installation](#installation)
- [Quick start](#quick-start)
- [Core retry semantics](#core-retry-semantics)
- [Budget configuration](#budget-configuration)
- [Retry policy](#retry-policy)
- [Sizing a budget](#sizing-a-budget)
- [Named budgets](#named-budgets)
- [Error handling](#error-handling)
- [Retry classification](#retry-classification)
- [HTTP integration](#http-integration)
- [Database writes](#database-writes)
- [Worker pools](#worker-pools)
- [Hooks and observability](#hooks-and-observability)
- [Concurrency guarantees](#concurrency-guarantees)
- [Testing and benchmarks](#testing-and-benchmarks)
- [Limitations](#limitations)
- [FAQ](#faq)

## Why retry budgets matter

Independent retry loops multiply traffic during the exact moment a dependency
needs less traffic.

Consider 200 workers, each configured for one initial attempt and four retries.
If a shared database becomes unavailable, the application can create as many as
800 additional attempts. Exponential backoff spreads those attempts over time,
but it does not limit their total number.

A shared budget changes the upper bound:

```text
without a shared budget: callers × retries = retry pressure
with a shared budget:    available tokens = retry pressure
```

If those workers share a budget with 50 tokens, only 50 retries can be admitted
before the dependency is given time to recover. Initial attempts remain free, so
normal application traffic is not blocked by Stormbreak.

Stormbreak complements backoff, rate limits, circuit breakers, and idempotency.
Its specific job is to coordinate retry capacity across callers.

## How Stormbreak works

```text
initial operation attempt (free)
          │
          ├─ success ───────────────────────────────► return result
          │
          └─ error
               │
               ├─ canceled / permanent / rejected ─► return error
               ├─ no attempts remain ──────────────► ErrAttemptsExhausted
               └─ budget.Allow()
                      │
                      ├─ denied ────────────────────► ErrBudgetExhausted
                      └─ allowed
                            │
                            └─ jittered backoff ────► next attempt
```

Budgets are token buckets that refill lazily. They do not create goroutines,
tickers, worker pools, or global state.

## Installation

Stormbreak requires Go 1.23 or newer.

Install the latest release:

```bash
go get github.com/magnexis/stormbreak
```

Pin the current release explicitly:

```bash
go get github.com/magnexis/stormbreak@v0.1.0
```

The HTTP transport is part of the same module:

```go
import "github.com/magnexis/stormbreak/httpretry"
```

Stormbreak has no third-party runtime dependencies.

## Quick start

```go
budget, err := stormbreak.NewBudget(stormbreak.Config{
    Capacity:       100,
    RefillRate:     10,
    RefillInterval: time.Second,
})
if err != nil {
    return err
}

result, err := stormbreak.Do(
    ctx,
    budget,
    stormbreak.Policy{
        MaxAttempts: 5,
        BaseDelay:   200 * time.Millisecond,
        MaxDelay:    5 * time.Second,
        Multiplier:  2,
        Jitter:      true,
    },
    func(ctx context.Context) (string, error) {
        return fetchRemoteResource(ctx)
    },
)
```

For operations that only return an error, use `DoVoid`:

```go
err = stormbreak.DoVoid(ctx, budget, stormbreak.DefaultPolicy(),
    func(ctx context.Context) error {
        return refreshCache(ctx)
    },
)
```

## Core retry semantics

Stormbreak follows these ordering rules for every execution:

1. Validate the context, budget, policy, operation, and options.
2. Execute attempt 1 immediately without consuming a token.
3. Stop if the context is canceled, the error is permanent, or the classifier
   rejects the error.
4. Stop with `ErrAttemptsExhausted` if the policy has no attempts remaining.
5. Ask the shared budget for one retry token.
6. Stop with `ErrBudgetExhausted` if no token is available.
7. Compute jittered exponential backoff and wait through the context.
8. Execute the next attempt.

Important details:

- `MaxAttempts` includes the initial attempt.
- Only retries consume tokens.
- A token is consumed when a retry is admitted, before backoff begins.
- A token is not refunded if the context is canceled during backoff.
- Cancellation wins if it races with an operation return.
- Exhaustion errors retain the final operation error for `errors.Is` and
  `errors.As` traversal.
- A custom `Budget` implementation can coordinate admission differently as long
  as it satisfies the public interface.

## Budget configuration

```go
type Config struct {
    Capacity       int64
    RefillRate     int64
    RefillInterval time.Duration
}
```

| Field | Meaning | Validation |
| --- | --- | --- |
| `Capacity` | Maximum and initial retry tokens | Must be greater than zero |
| `RefillRate` | Tokens restored per completed interval | Cannot be negative |
| `RefillInterval` | Time represented by one refill interval | Must be positive when refill is enabled |

Examples:

```go
// Recovers ten retry tokens every second, up to 100.
burstBudget, _ := stormbreak.NewBudget(stormbreak.Config{
    Capacity:       100,
    RefillRate:     10,
    RefillInterval: time.Second,
})

// Static budget: tokens return only after Reset.
deploymentBudget, _ := stormbreak.NewBudget(stormbreak.Config{
    Capacity: 20,
})
```

Budgets begin full. `Reset` restores full capacity and restarts the refill
window. Time spent at full capacity is not banked for an immediate refill after
a token is consumed.

### Snapshots

```go
snapshot := budget.Snapshot()
fmt.Printf("remaining=%d capacity=%d exhausted=%t\n",
    snapshot.Remaining,
    snapshot.Capacity,
    snapshot.Exhausted,
)
```

`BudgetSnapshot` is a value copy. Mutating it cannot change the budget.

## Retry policy

```go
type Policy struct {
    MaxAttempts int
    BaseDelay   time.Duration
    MaxDelay    time.Duration
    Multiplier  float64
    Jitter      bool
}
```

| Field | Meaning | Validation |
| --- | --- | --- |
| `MaxAttempts` | Initial attempt plus retries | At least 1 |
| `BaseDelay` | Backoff cap before retry 1 | Cannot be negative |
| `MaxDelay` | Maximum exponential delay cap | At least `BaseDelay` |
| `Multiplier` | Exponential growth factor | `0` selects `2`; otherwise at least `1` |
| `Jitter` | Uniformly randomize between zero and the calculated cap | Boolean |

`DefaultPolicy` returns:

| Setting | Default |
| --- | ---: |
| Maximum attempts | 3 |
| Base delay | 200 ms |
| Maximum delay | 5 s |
| Multiplier | 2 |
| Full jitter | Enabled |

When jitter is enabled, the delay is uniformly distributed in the range from
zero up to the calculated exponential cap. `WithRandomSource` makes jitter
deterministic in tests without introducing package-level mutable state.

## Sizing a budget

A useful budget is tied to a dependency and a recovery objective—not copied from
the retry count.

Start with three questions:

1. How many additional attempts can the dependency tolerate during a failure?
2. How quickly should retry capacity return after recovery begins?
3. How many independent processes or replicas are making calls?

For example:

```text
capacity:        50 retries of immediate burst tolerance
refill rate:      5 retries
refill interval:  1 second
maximum refill:   5 retries/second per process
```

Local budgets coordinate only one process. If ten replicas each use that
configuration, the fleet can admit up to 500 burst retries and refill up to 50
tokens per second. Size local budgets with replica count in mind until a
distributed budget is introduced.

Prefer separate budgets when dependencies fail independently. Share one budget
when several call paths ultimately pressure the same constrained resource.

## Named budgets

`Registry` manages dependency-level budgets without creating global state:

```go
registry := stormbreak.NewRegistry()

githubBudget, err := registry.Create("github-api", stormbreak.Config{
    Capacity:       30,
    RefillRate:     3,
    RefillInterval: time.Second,
})
if err != nil {
    return err
}

databaseBudget, err := registry.Create("database", stormbreak.Config{
    Capacity:       10,
    RefillRate:     1,
    RefillInterval: time.Second,
})
```

Registry names are unique. `Names` returns a sorted copy, and `Snapshot` returns
detached snapshots for every registered budget.

## Error handling

Terminal retry errors use `RetryError`:

```go
type RetryError struct {
    Attempts  int
    LastError error
    Cause     error
}
```

```go
result, err := stormbreak.Do(ctx, budget, policy, operation)
if errors.Is(err, stormbreak.ErrBudgetExhausted) {
    // Shed work, use a fallback, or surface dependency pressure.
}
if errors.Is(err, stormbreak.ErrAttemptsExhausted) {
    // This operation used every policy attempt.
}

var retryErr *stormbreak.RetryError
if errors.As(err, &retryErr) {
    log.Printf("attempts=%d cause=%v last_error=%v",
        retryErr.Attempts,
        retryErr.Cause,
        retryErr.LastError,
    )
}
_ = result
```

| Stop condition | Returned error |
| --- | --- |
| Operation succeeds | `nil` |
| Context canceled or deadline exceeded | Context error |
| Permanent error | Permanent wrapper, preserving the original error |
| Classifier rejects error | Original operation error |
| Policy attempts exhausted | `*RetryError` wrapping `ErrAttemptsExhausted` and the final error |
| Shared budget exhausted | `*RetryError` wrapping `ErrBudgetExhausted` and the final error |
| Invalid budget or policy | Descriptive error wrapping `ErrInvalidBudget` or `ErrInvalidPolicy` |

## Retry classification

Ordinary errors are retryable by default unless they are permanent or represent
context cancellation.

Mark a known permanent failure at its source:

```go
return stormbreak.Permanent(errors.New("invalid credentials"))
```

Or install a domain-specific classifier:

```go
err := stormbreak.DoVoid(ctx, budget, policy, operation,
    stormbreak.WithClassifier(func(err error) bool {
        return errors.Is(err, ErrTemporarilyUnavailable)
    }),
)
```

`Permanent(nil)` remains `nil`. Permanent errors stop without consuming retry
capacity. `AlwaysRetry` and `NeverRetry` are available for explicit policies.

## HTTP integration

The optional `httpretry` package provides an `http.RoundTripper` protected by a
shared Stormbreak budget:

```go
client := &http.Client{
    Timeout: 10 * time.Second,
    Transport: &httpretry.Transport{
        Budget:        budget,
        Policy:        stormbreak.DefaultPolicy(),
        MaxRetryAfter: 30 * time.Second,
    },
}
```

If `Base` is nil, the transport delegates to `http.DefaultTransport`.

### Default response classification

| Condition | Retried by default |
| --- | :---: |
| Temporary or timeout network error | Yes |
| `io.EOF` or `io.ErrUnexpectedEOF` | Yes |
| HTTP 408 Request Timeout | Yes |
| HTTP 425 Too Early | Yes |
| HTTP 429 Too Many Requests | Yes |
| HTTP 500, 502, 503, or 504 | Yes |
| Authentication or authorization 4xx | No |
| Validation and other general 4xx | No |
| Context cancellation | No |

A custom `Transport.Classifier` can change response and transport-error
classification. Request replay and method-safety checks still apply.

### Method and body safety

| Method | Retry condition |
| --- | --- |
| GET, HEAD, OPTIONS, TRACE | Body absent or replayable |
| PUT, DELETE | Body absent or replayable |
| POST, PATCH | `Idempotency-Key` present and body absent or replayable |
| Other methods | Not retried by default |

A body is replayable only when `Request.GetBody` can create a fresh reader. Go's
`http.NewRequest` configures `GetBody` automatically for common in-memory readers
such as `bytes.Buffer`, `bytes.Reader`, and `strings.Reader`.

An `Idempotency-Key` header is only a signal to Stormbreak. The upstream service
must actually implement idempotency for the key.

### Retry-After

The transport accepts both integer seconds and HTTP-date values. The server
delay can extend normal backoff but is bounded by `MaxRetryAfter`. A zero
`MaxRetryAfter` selects the safe one-minute default. The request context or
`http.Client.Timeout` remains the ultimate deadline.

### HTTP response ownership

Intermediate retry responses are drained by a small bounded amount and closed.
The final returned response remains the caller's responsibility:

```go
response, err := client.Do(request)
if err != nil {
    return err
}
defer response.Body.Close()
```

When retryable status responses use every policy attempt, the final response is
returned normally, following `net/http` conventions. If the shared budget stops
the request first, Stormbreak closes the response and returns
`ErrBudgetExhausted` with a typed `*httpretry.StatusError` as the last error.

## Database writes

Retry only errors the driver or database identifies as transient. A write may
need a complete transaction restart, uniqueness constraint, compare-and-swap,
or application idempotency key.

```go
err := stormbreak.DoVoid(
    ctx,
    databaseBudget,
    stormbreak.Policy{
        MaxAttempts: 4,
        BaseDelay:   100 * time.Millisecond,
        MaxDelay:    time.Second,
        Multiplier:  2,
        Jitter:      true,
    },
    func(ctx context.Context) error {
        return repository.Save(ctx, record)
    },
    stormbreak.WithClassifier(isTemporaryDatabaseError),
)
```

Do not retry an entire write merely because its outcome is unknown. A timeout
can occur after the database committed the change.

## Worker pools

Give every worker that reaches the same dependency the same budget:

```go
err := stormbreak.DoVoid(ctx, queueBudget, workerPolicy,
    func(ctx context.Context) error {
        return processor.Handle(ctx, message)
    },
)
```

If that dependency fails, retry volume is bounded by shared capacity instead of
worker count multiplied by retries. Message acknowledgement, visibility
timeouts, deduplication, poison-message handling, and dead-letter behavior
remain queue concerns.

## Hooks and observability

Hooks provide synchronous, dependency-free integration points:

```go
hooks := stormbreak.Hooks{
    OnRetry: func(event stormbreak.RetryEvent) {
        log.Printf("attempt=%d delay=%s remaining=%d err=%v",
            event.Attempt,
            event.Delay,
            event.BudgetRemaining,
            event.Error,
        )
    },
}

result, err := stormbreak.Do(ctx, budget, policy, operation,
    stormbreak.WithHooks(hooks),
)
```

| Hook | Emitted when |
| --- | --- |
| `OnAttempt` | Immediately before each operation attempt |
| `OnRetry` | A retry token is consumed and backoff is about to begin |
| `OnSuccess` | An operation or transport completes successfully |
| `OnFailure` | An operation attempt or retryable HTTP response fails |
| `OnBudgetExhausted` | The budget denies a retry |

`httpretry.Transport.Hooks` uses the same event types. Retryable HTTP statuses
appear as `*httpretry.StatusError`:

```go
transport.Hooks = stormbreak.Hooks{
    OnFailure: func(event stormbreak.FailureEvent) {
        var statusErr *httpretry.StatusError
        if errors.As(event.Error, &statusErr) {
            log.Printf("retryable_status=%d", statusErr.StatusCode)
        }
    },
}
```

Hooks are nil-safe and run in the calling path. They should return quickly. If
telemetry can block, enqueue it into a bounded application-owned channel. Hooks
may execute concurrently when multiple callers share a budget or transport.

## Concurrency guarantees

- `TokenBudget` methods are safe for concurrent use.
- Mutable token and refill-clock state is protected by one mutex.
- A budget never invokes hooks or operations while holding its lock.
- Refills are lazy; no goroutine or ticker belongs to a budget.
- `Registry` creation, lookup, deletion, names, and snapshots are concurrency-safe.
- `Do` stores execution state locally and creates no background goroutine.
- `httpretry.Transport` is safe for concurrent requests when its fields are not
  mutated after first use and its collaborators are concurrency-safe.
- Hook callback state remains the application's responsibility.

## Comparison with an ordinary retry loop

| Capability | Basic retry loop | Stormbreak |
| --- | ---: | ---: |
| Exponential backoff | Manual | Yes |
| Context cancellation | Often missing | Yes |
| Shared retry limits | No | Yes |
| Retry classification | Manual | Yes |
| Permanent errors | Manual | Yes |
| Full jitter | Manual | Yes |
| Observability hooks | Manual | Yes |
| HTTP integration | Manual | Yes |
| Concurrent safety | Uncertain | Yes |
| Dependency-level budgets | No | Yes |
| Lazy refill without goroutines | Manual | Yes |

Stormbreak does not claim to replace other retry libraries. Choose it when
coordinating aggregate retry pressure is a first-class requirement.

## Design philosophy

- The first attempt is not a retry and never consumes budget.
- Capacity is explicit and dependency-scoped.
- Local mode requires no account, daemon, or remote configuration.
- Context cancellation is checked before attempts, after operation returns, and
  during backoff.
- Invalid configuration fails instead of silently changing behavior.
- Standard-library primitives keep the dependency and operational footprint small.
- Extension points remain narrow: `Budget`, classifiers, hooks, and HTTP transport.

Implementation invariants and the HTTP lifecycle are documented in
[ARCHITECTURE.md](ARCHITECTURE.md).

## Examples

| Example | Demonstrates |
| --- | --- |
| [`examples/basic`](examples/basic) | Typed retry execution |
| [`examples/shared-budget`](examples/shared-budget) | Concurrent callers sharing capacity |
| [`examples/http-client`](examples/http-client) | HTTP transport and hooks |
| [`examples/database-retry`](examples/database-retry) | Classified, idempotent writes |
| [`examples/worker-pool`](examples/worker-pool) | Worker-level dependency protection |

Public API examples also run as Go example tests and appear in generated
[package documentation](https://pkg.go.dev/github.com/magnexis/stormbreak).

## Testing and benchmarks

Run the same core checks used by CI:

```bash
gofmt -l .
go test -count=1 -shuffle=on ./...
go test -race -count=1 ./...
go vet ./...
```

Run bounded fuzzing:

```bash
go test -run '^$' -fuzz '^FuzzBackoffBounds$' -fuzztime=10s .
go test -run '^$' -fuzz '^FuzzRetryAfterDelay$' -fuzztime=10s ./httpretry
```

Run benchmarks:

```bash
go test -run '^$' -bench . -benchmem .
```

Reference run on 2026-08-02 using Go 1.26.3 on Windows/amd64 and an AMD Ryzen 9
9950X with 32 logical CPUs:

| Benchmark | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| `BenchmarkBudgetAllow-32` | 11.26 | 0 | 0 |
| `BenchmarkBudgetAllowParallel-32` | 39.18 | 0 | 0 |
| `BenchmarkDoSuccess-32` | 25.03 | 64 | 1 |
| `BenchmarkDoSingleRetry-32` | 123.4 | 96 | 4 |
| `BenchmarkRegistryGet-32` | 10.26 | 0 | 0 |

These figures are one local reference run, not a performance guarantee.
Compiler, hardware, contention, hooks, classifiers, and operation behavior can
materially change results.

CI covers the minimum supported Go version, oldstable, stable, Windows, macOS,
race detection, fuzz smoke tests, vet, formatting, and CodeQL.

## Limitations

- Budgets coordinate one process only.
- Stormbreak is not a circuit breaker, rate limiter, scheduler, queue, workflow
  engine, service mesh, or distributed lock.
- It cannot make non-idempotent operations safe to repeat.
- It cannot determine whether a timed-out remote write actually completed.
- It does not coordinate capacity across replicas.
- It does not provide mandatory logging, metrics, tracing, or telemetry.
- Refill uses the process clock, and `Reset` intentionally discards the current
  refill window.

## FAQ

### Is a retry budget the same as a rate limiter?

No. A rate limiter controls all admitted traffic. Stormbreak controls only
additional retry attempts; initial attempts are free.

### Is Stormbreak a circuit breaker?

No. A circuit breaker suppresses initial calls while a dependency is unhealthy.
Stormbreak limits retry amplification. The two patterns can be composed.

### Should I create one budget per operation?

Usually not. Create one budget per dependency or shared failure domain so
independent call sites coordinate their retry pressure.

### Does a canceled retry return its token?

No. The token represents admission of the retry and is consumed before backoff.
Cancellation does not refund it.

### Can Stormbreak retry POST requests?

The HTTP transport retries POST and PATCH only when an `Idempotency-Key` is
present and the body is replayable through `Request.GetBody`. The server must
enforce the key.

### Why is there no `go.sum`?

The module has no dependencies outside the Go standard library, so `go mod tidy`
does not generate one.

### Can several application replicas share one budget?

Not in `v0.1.0`. Each process has its own local budget. Distributed coordination
is deliberately outside the initial release.

## Roadmap

Potential future additions include circuit-breaker interoperability,
OpenTelemetry helpers, Prometheus adapters, distributed Redis-backed budgets,
adaptive refill rates, server-provided retry budgets, gRPC interceptors, SQL and
queue adapters, per-error retry costs, and dynamic policy updates.

Distributed coordination will remain optional and outside the core package.

## Contributing, support, and security

- [Contributing guide](CONTRIBUTING.md)
- [Support policy](SUPPORT.md)
- [Security policy](SECURITY.md)
- [Release process](RELEASING.md)
- [Code of conduct](CODE_OF_CONDUCT.md)
- [Changelog](CHANGELOG.md)

Bug reports and feature proposals use the repository's structured GitHub issue
forms. Security vulnerabilities must be reported through private vulnerability
reporting, not public issues.

## License

Stormbreak is available under the [MIT License](LICENSE).
