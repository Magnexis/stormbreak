# Support

Stormbreak is maintained as an open-source Magnexis project.

## Questions and usage help

Before opening an issue:

1. Read the [README](README.md), especially HTTP idempotency and limitations.
2. Review [ARCHITECTURE.md](ARCHITECTURE.md) for token and attempt semantics.
3. Search existing GitHub issues for the same behavior.
4. Reduce the problem to a small reproducible Go program when possible.

Use a GitHub issue for reproducible bugs and focused feature proposals. Include
the Stormbreak version, Go version, platform, policy and budget configuration,
expected behavior, and actual behavior. Remove credentials, private endpoints,
database contents, and production payloads.

## Security issues

Do not report vulnerabilities in a public issue. Follow [SECURITY.md](SECURITY.md)
and use GitHub private vulnerability reporting.

## Support boundaries

Maintainers can help with Stormbreak behavior and documented integrations, but
cannot determine whether an application write is idempotent, administer an
upstream API, recover queue messages, or diagnose private infrastructure without
a safe standalone reproduction.
