# Changelog

All notable changes to Stormbreak are documented here. The project follows
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- Added an original Stormbreak logo and linked the README identity directly to
  the module's official `pkg.go.dev` page.
- Expanded README with detailed semantics, configuration references, operational
  sizing guidance, HTTP safety matrices, FAQ, navigation, and release badges.

- Executable GoDoc examples and a database retry example.
- Architecture documentation covering retry flow, invariants, and HTTP safety.
- Deterministic internal timing seams for exact backoff and `Retry-After` tests.
- HTTP transport observability through the core hook event model and typed
  `httpretry.StatusError` values.
- End-to-end `http.Client` integration tests and fuzz targets for backoff and
  `Retry-After` parsing.
- A release runbook with local and post-publish verification.

### Changed

- HTTP `Retry-After` delays are bounded to one minute by default and configurable
  through `Transport.MaxRetryAfter`.
- Native and custom typed-nil budgets are rejected before execution.
- HTTP transport contract violations now return descriptive errors and close any
  unusable response body.
- Retry error formatting now handles partially populated user-created values.
- CI now tests Windows and macOS separately, shuffles unit tests, runs race
  detection as a dedicated job, and performs bounded fuzz smoke tests.

### Fixed

- Invalid NaN jitter sources can no longer produce negative durations.
- Empty HTTP methods now follow `net/http` semantics and are treated as GET for
  retry safety.
- Context cancellation now wins consistently when it races with a successful
  operation return.

## [0.1.0] - 2026-08-01

### Added

- Concurrency-safe, lazily refilled token budgets.
- Generic and error-only retry execution APIs.
- Exponential backoff with full jitter and context cancellation.
- Retry classification, permanent errors, hooks, and structured errors.
- Concurrency-safe named budget registry and snapshots.
- Optional HTTP retry transport with replay and `Retry-After` support.
- Examples, behavioral tests, race tests, benchmarks, and project documentation.

[Unreleased]: https://github.com/magnexis/stormbreak/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/magnexis/stormbreak/releases/tag/v0.1.0
