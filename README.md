<div align="center">

# STORMBREAK

**Shared retry budgets for resilient Go applications.**

[![CI](https://github.com/magnexis/stormbreak/actions/workflows/ci.yml/badge.svg)](https://github.com/magnexis/stormbreak/actions/workflows/ci.yml)
[![CodeQL](https://github.com/magnexis/stormbreak/actions/workflows/codeql.yml/badge.svg)](https://github.com/magnexis/stormbreak/actions/workflows/codeql.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/magnexis/stormbreak.svg)](https://pkg.go.dev/github.com/magnexis/stormbreak)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

</div>

Stormbreak prevents failing services from being overwhelmed by the retries
intended to save them.

> **Project status:** release candidate for `v0.1.0`. The API is intentionally
> small but remains pre-1.0; review the changelog when upgrading minor versions.

Ordinary retry loops make decisions in isolation. Under load, hundreds of HTTP
requests, workers, or database operations can all retry together, amplifying the
failure. Stormbreak keeps retry policy local to an operation while sharing a
concurrency-safe token budget across every caller that depends on the same
resource.

## Installation

Stormbreak requires Go 1.23 or newer.

```bash
go get github.com/magnexis/stormbreak
```

It has no third-party runtime dependencies.

## Basic usage

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

The first attempt is always free. Each retry consumes one token. Tokens return
lazily at the configured rate, without a refill goroutine.

## Share budgets by dependency

One budget should represent one constrained dependency or failure domain, not
one call site. A registry is useful when an application has several of them:

```go
registry := stormbreak.NewRegistry()

githubBudget, err := registry.Create("github-api", stormbreak.Config{
    Capacity: 30, RefillRate: 3, RefillInterval: time.Second,
})
if err != nil {
    return err
}

databaseBudget, err := registry.Create("database", stormbreak.Config{
    Capacity: 10, RefillRate: 1, RefillInterval: time.Second,
})
```

Pass `githubBudget` to every GitHub caller and `databaseBudget` to every database
caller. `Registry.Snapshot` and `TokenBudget.Snapshot` expose immutable state for
monitoring.

## HTTP client

The optional `httpretry` package retries temporary network failures and status
codes 408, 425, 429, 500, 502, 503, and 504 by default.

```go
client := &http.Client{
    Timeout: 10 * time.Second,
    Transport: &httpretry.Transport{
        Budget:        budget,
        Policy:        stormbreak.DefaultPolicy(),
        MaxRetryAfter: 30 * time.Second, // zero defaults to one minute
    },
}
```

The transport honors `Retry-After`, closes intermediate response bodies, and
replays bodies only through `Request.GetBody`. Server-requested delays are
bounded by `MaxRetryAfter` so an untrusted header cannot create an effectively
unbounded timer; the request context remains the ultimate deadline. It does not
retry authentication or general validation failures by default.

HTTP retries do not make writes safe. The default transport retries idempotent
methods. `POST` and `PATCH` additionally require an `Idempotency-Key` header and
a replayable body, but the upstream server must actually enforce that key. A
custom `Transport.Classifier` can alter response/error classification; request
safety checks still apply.

The same hooks used by `Do` can observe the HTTP transport:

```go
transport := &httpretry.Transport{
    Budget: budget,
    Policy: stormbreak.DefaultPolicy(),
    Hooks: stormbreak.Hooks{
        OnFailure: func(event stormbreak.FailureEvent) {
            var statusErr *httpretry.StatusError
            if errors.As(event.Error, &statusErr) {
                metrics.RecordRetryableStatus(statusErr.StatusCode)
            }
        },
    },
}
```

Retryable HTTP responses appear in hooks as `*httpretry.StatusError`. A final
retryable response is still returned normally after policy attempts are used,
following `net/http` conventions. If the shared budget stops the request first,
the status error is retained as `RetryError.LastError`.

## Error handling

When policy attempts or shared capacity run out, Stormbreak returns a
`*RetryError`. Both its cause and final operation error participate in
`errors.Is` and `errors.As`.

```go
result, err := stormbreak.Do(ctx, budget, policy, operation)
if errors.Is(err, stormbreak.ErrBudgetExhausted) {
    // Fail fast, shed work, or surface dependency pressure.
}
if errors.Is(err, stormbreak.ErrAttemptsExhausted) {
    // This operation used every permitted attempt.
}

var retryErr *stormbreak.RetryError
if errors.As(err, &retryErr) {
    log.Printf("attempts=%d last_error=%v", retryErr.Attempts, retryErr.LastError)
}
_ = result
```

Invalid budgets and policies wrap `ErrInvalidBudget` and `ErrInvalidPolicy` with
descriptive context. A policy multiplier of zero uses the safe default of `2`,
which keeps concise policy literals useful; any explicit multiplier must be at
least `1`.

## Retry classification

Ordinary errors are retryable by default unless the context is canceled. Mark
known permanent failures at their source:

```go
return stormbreak.Permanent(errors.New("invalid credentials"))
```

Or install a domain classifier:

```go
err := stormbreak.DoVoid(ctx, budget, policy, operation,
    stormbreak.WithClassifier(func(err error) bool {
        return errors.Is(err, ErrTemporarilyUnavailable)
    }),
)
```

`AlwaysRetry` and `NeverRetry` are provided for explicit policies. Permanent
errors stop immediately, before any retry token is consumed.

## Database writes

Retry only errors the driver or database identifies as transient. A retried
write may need a transaction restart, uniqueness constraint, or application
idempotency key.

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

## Worker pools

Give every worker that reaches the same dependency the same budget:

```go
err := stormbreak.DoVoid(ctx, queueBudget, workerPolicy,
    func(ctx context.Context) error {
        return processor.Handle(ctx, message)
    },
)
```

If that dependency fails, only the shared token capacity can become retries—not
the number of workers multiplied by each worker's maximum attempts. Message
acknowledgement, deduplication, and dead-letter behavior remain queue concerns.

## Hooks and observability

```go
hooks := stormbreak.Hooks{
    OnRetry: func(event stormbreak.RetryEvent) {
        retries.Add(ctx, 1)
        logger.Printf("attempt=%d delay=%s remaining=%d err=%v",
            event.Attempt, event.Delay, event.BudgetRemaining, event.Error)
    },
}

result, err := stormbreak.Do(ctx, budget, policy, operation,
    stormbreak.WithHooks(hooks),
)
```

Hooks are optional, nil-safe, synchronous, and dependency-free. They should
return quickly and must not block critical application paths. If exporting
telemetry can block, enqueue it in a bounded application-owned channel.

## Concurrency guarantees

- `TokenBudget` methods are safe for concurrent use.
- Refills are calculated lazily while holding the budget lock; no background
  goroutine or ticker is created.
- `Registry` operations and snapshots are safe during concurrent creation,
  lookup, and deletion.
- `Do` keeps all execution state local to the call.
- Hooks and operations are invoked synchronously; their own shared state remains
  the application's responsibility.
- `httpretry.Transport` is safe for concurrent requests when its fields are not
  mutated after first use and its `Base`, budget, classifier, and hooks satisfy
  their own concurrency requirements. Hooks may run concurrently across calls.

## Why not a basic loop?

| Capability               | Basic retry loop | Stormbreak |
| ------------------------ | ---------------: | ---------: |
| Exponential backoff      |           Manual |        Yes |
| Context cancellation     |    Often missing |        Yes |
| Shared retry limits      |               No |        Yes |
| Retry classification     |           Manual |        Yes |
| Jitter                   |           Manual |        Yes |
| Observability hooks      |           Manual |        Yes |
| HTTP integration         |           Manual |        Yes |
| Concurrent safety        |        Uncertain |        Yes |
| Dependency-level budgets |               No |        Yes |

Stormbreak is intentionally compatible with other resilience techniques. Its
distinct role is coordinating retry pressure across callers.

## Design philosophy

- The first attempt is not a retry and never consumes budget.
- Capacity is local and explicit; there is no global registry or mutable policy.
- Full jitter spreads retry timing between zero and the exponential delay cap.
- Context cancellation wins before execution and during backoff.
- Standard-library primitives keep the dependency and allocation footprint
  small.
- Invalid configuration fails at construction or execution instead of silently
  changing behavior.

The implementation invariants and HTTP request lifecycle are described in
[ARCHITECTURE.md](ARCHITECTURE.md).

## Examples

- [`examples/basic`](examples/basic) — typed retry execution
- [`examples/shared-budget`](examples/shared-budget) — concurrent callers sharing capacity
- [`examples/http-client`](examples/http-client) — HTTP transport integration
- [`examples/database-retry`](examples/database-retry) — classified, idempotent writes
- [`examples/worker-pool`](examples/worker-pool) — worker-level dependency protection

Public API examples also run as Go example tests and appear in generated package
documentation.

## Limitations

The initial release coordinates retries only within one process. It is not a
circuit breaker, rate limiter, scheduler, queue, or distributed budget. It
cannot guarantee idempotency, prevent a remote operation from completing after
a network timeout, or coordinate independent application replicas. Refill uses
the process clock; `Reset` intentionally discards the current refill window.

## Benchmarks

Benchmarks cover serial and parallel budget access, first-attempt success, one
retry, and registry lookup. Results depend heavily on CPU, Go version, and race
instrumentation; run them on the target environment instead of treating checked
figures as a guarantee:

```bash
go test -run '^$' -bench . -benchmem ./...
```

No throughput or allocation target is part of the public compatibility promise.

Reference run on 2026-08-02 using Go 1.26.3 on Windows/amd64 and an AMD Ryzen 9
9950X (32 logical CPUs):

| Benchmark | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| `BenchmarkBudgetAllow-32` | 11.26 | 0 | 0 |
| `BenchmarkBudgetAllowParallel-32` | 39.18 | 0 | 0 |
| `BenchmarkDoSuccess-32` | 25.03 | 64 | 1 |
| `BenchmarkDoSingleRetry-32` | 123.4 | 96 | 4 |
| `BenchmarkRegistryGet-32` | 10.26 | 0 | 0 |

These are one local reference run, not a performance guarantee. Compiler,
hardware, contention, hooks, classifiers, and operation behavior can materially
change results.

## Roadmap

Possible future additions include circuit-breaker interoperability,
OpenTelemetry helpers, Prometheus adapters, Redis-backed distributed budgets,
adaptive refill rates, server-provided retry budgets, gRPC interceptors, SQL and
queue adapters, per-error retry costs, and dynamic policy updates. Distributed
coordination will remain optional and outside the core package.

## Contributing and release

See [CONTRIBUTING.md](CONTRIBUTING.md), [SUPPORT.md](SUPPORT.md),
[SECURITY.md](SECURITY.md), and [RELEASING.md](RELEASING.md). The normal release
checks are:

```bash
go test -count=1 -shuffle=on ./...
go test -race -count=1 ./...
go vet ./...
```

After updating the changelog, a maintainer can create the first release with:

```bash
git tag v0.1.0
git push origin v0.1.0
```

## License

Stormbreak is available under the [MIT License](LICENSE).
