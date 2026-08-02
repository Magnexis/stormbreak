# Stormbreak architecture

Stormbreak is intentionally a small in-process coordination module. The core
abstraction is a retry token budget shared by callers that depend on the same
failure domain.

## Execution flow

```text
operation attempt 1 (free)
        │
        ├─ success ───────────────────────────────► return result
        │
        └─ error
             │
             ├─ context canceled/permanent/classified false ─► return error
             ├─ maximum attempts reached ─────────────────────► RetryError
             └─ Budget.Allow()
                    │
                    ├─ denied ─────────────────────────────────► RetryError
                    └─ allowed ─► jittered backoff ─► next attempt
```

The operation owns application behavior. The policy owns per-call timing and
attempt limits. The budget owns aggregate retry pressure. Keeping these concerns
separate lets HTTP clients, workers, and repositories coordinate without sharing
operation-specific code.

## Budget invariants

- The initial token count equals capacity.
- Remaining tokens never exceed capacity or fall below zero.
- Only retries consume tokens; initial attempts do not.
- Refill is lazy and interval-based. No goroutine or ticker belongs to a budget.
- Time spent at full capacity is discarded rather than banked.
- All mutable token and refill-clock state is protected by one mutex.
- Capacity and refill configuration are immutable after construction.

The mutex is deliberately narrow. A budget never invokes operations, hooks, or
external code while holding it.

## Retry execution

`Do` validates inputs once, then retains classifier, hook, random, and wait state
within that invocation. No execution state is global. Context is checked before
the first attempt, before every later attempt, immediately after every operation
return, and by the timer used for backoff. Cancellation therefore wins even when
it races with a nominally successful result.

`RetryError` preserves two distinct facts: the terminal control cause
(`ErrBudgetExhausted` or `ErrAttemptsExhausted`) and the last operation error.
Both are available through Go's `errors.Is` and `errors.As` traversal.

## HTTP transport lifecycle

`httpretry.Transport` composes an existing `http.RoundTripper`; it does not
replace connection pooling or TLS behavior. Requests are retried only when all
of these conditions hold:

1. The classifier accepts the response or transport error.
2. The method is idempotent, or POST/PATCH supplies an `Idempotency-Key`.
3. A request body is absent or replayable through `Request.GetBody`.
4. Policy attempts remain.
5. The shared budget grants a retry token.

Before a retry, an intermediate response is drained by a small bounded amount
and closed. `Retry-After` can extend normal backoff but is bounded by
`Transport.MaxRetryAfter` (one minute by default), preventing untrusted headers
from creating effectively unbounded timers. The request context remains the
ultimate deadline.

HTTP observability uses the core hook event types. A response accepted by the
classifier as retryable emits `OnFailure` with `*httpretry.StatusError`; a retry
token emits `OnRetry`; budget denial emits `OnBudgetExhausted`; and a transport-
level success emits `OnSuccess`. Non-retryable HTTP responses such as 401 remain
successful round trips, even though application code may reject their status.
Transport configuration is immutable by convention after first use; hook
callbacks may execute concurrently when an `http.Client` serves concurrent
requests.

## Extension boundaries

The `Budget` interface is the intended extension point for alternative local or
distributed coordination. External adapters should remain separate packages so
the core keeps its standard-library-only dependency graph. Circuit breakers,
metrics exporters, SQL helpers, gRPC interceptors, and distributed stores should
compose with retry budgets rather than expand the core into a general resilience
framework.
