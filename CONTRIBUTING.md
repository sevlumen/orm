# Contributing

Contributions are welcome when they preserve the project's explicit SQL, compatibility, performance, and security contracts.

## Before opening a change

- Use an issue for a new public API, persistent format, database feature, or compatibility change.
- Use private security reporting for potential vulnerabilities.
- Keep runtime dependencies minimal and justify any new dependency.
- Prefer generated code and standard-library tooling over reflection or runtime magic.
- Do not introduce lazy loading, hidden N+1 queries, automatic production schema mutation, or request-derived raw SQL.

## Development requirements

Use Go 1.25 or newer and PostgreSQL 14 or 18 for local integration testing.

```text
go mod tidy
git diff --exit-code -- go.mod go.sum
go mod verify
gofmt -w .
go vet ./...
go test -race ./... -count=1
```

PostgreSQL tests require:

```text
SEVLUMEN_TEST_DATABASE_URL='postgres://postgres:postgres@127.0.0.1:5432/sevlumen_test?sslmode=disable' \
go test -race ./postgres/... -count=1
```

Run the security and compatibility checks described in [Security testing](docs/security-testing.md). Longer fuzz campaigns are expected for parser, tokenizer, filesystem, and SQL-boundary changes.

## Pull requests

A pull request should:

- solve one reviewable problem;
- include regression tests for defects;
- include PostgreSQL integration coverage for database behavior;
- state SQL, locking, transaction, compatibility, and data-loss implications;
- avoid printing query arguments or credentials;
- update documentation and examples when behavior changes;
- preserve deterministic output;
- pass Go 1.25/stable and PostgreSQL 14/18 gates.

Do not weaken or skip a failing security, fuzz, race, checksum, API, or integration test to obtain a green build. Understand and fix the finding or explicitly revise the documented contract with review.

## Public API changes

The reviewed API surface is under `api/v1/`. Before `v1.0.0`, intentional changes require compatibility review and baseline regeneration. After `v1.0.0`, incompatible changes require a future major version except for narrowly justified security emergencies.

```text
go run ./cmd/apicheck -write -baseline api/v1
```

Review the generated diff; do not regenerate merely to silence CI.

## Dependencies

Dependency changes must include vulnerability analysis, module verification, full tests, and benchmark review for hot-path code. GitHub Actions use immutable SHAs, container evidence uses digests, and `go run` tooling uses exact versions.

## Commit and review hygiene

- Do not commit credentials, production snapshots, private SQL, customer data, fuzz inputs containing real data, or generated release secrets.
- Use descriptive commits and retain evidence in the pull request.
- Resolve review findings rather than hiding them in follow-up work.
- Keep generated files synchronized and verify codegen check mode.

## Conduct

Participation is governed by [the code of conduct](CODE_OF_CONDUCT.md).
