# Security Policy

## Supported versions

Until Stormbreak reaches 1.0, security fixes are provided for the latest minor
release.

## Reporting a vulnerability

Please use GitHub's private vulnerability reporting for this repository. Do not
open a public issue containing exploit details, credentials, or sensitive logs.
Include the affected version, impact, reproduction steps, and any suggested
mitigation. Maintainers will acknowledge a report as soon as practical.

## Security scope

Stormbreak controls retry traffic; it is not a circuit breaker, rate limiter,
distributed lock, or idempotency system. Applications must still validate
inputs, protect credentials, bound request lifetimes, and make retried writes
safe to repeat.

For HTTP integrations:

- Always give requests a context deadline or configure `http.Client.Timeout`.
- Keep `MaxRetryAfter` bounded when server headers cross a trust boundary.
- Do not assume an `Idempotency-Key` is effective unless the upstream service
  documents and enforces it.
- Treat custom classifiers as policy code: retrying authentication, validation,
  or authorization failures can amplify abuse and account lockouts.

Hooks run synchronously in the calling path. They must not perform unbounded
network I/O, include secrets in logs, or recursively invoke the same protected
dependency.
