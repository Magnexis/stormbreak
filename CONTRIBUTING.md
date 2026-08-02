# Contributing to Stormbreak

Thank you for helping improve Stormbreak. Keep changes focused on shared retry
budgets and small, composable integrations.

For usage questions and report preparation, see [SUPPORT.md](SUPPORT.md). Report
security issues privately according to [SECURITY.md](SECURITY.md).

## Development

Stormbreak requires Go 1.23 or newer and has no third-party runtime dependencies.

```bash
go test ./...
go test -race ./...
go vet ./...
gofmt -w .
```

Add behavioral tests for fixes and public API changes. Tests should be
deterministic: use `WithRandomSource` when jitter is involved and avoid timing
assertions when a fake clock or direct calculation will do.

Read [ARCHITECTURE.md](ARCHITECTURE.md) before changing token accounting,
attempt ordering, error unwrapping, or HTTP response-body ownership. Those
behaviors are compatibility constraints rather than implementation details.

## Pull requests

Explain the failure mode addressed, note compatibility implications, and keep
unrelated refactors separate. Update GoDoc, examples, and the changelog when the
public behavior changes. By participating, you agree to follow the code of
conduct.
