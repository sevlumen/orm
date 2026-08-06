# Changelog

All notable changes to Sevlumen ORM are documented here. The project follows semantic versioning beginning with `v1.0.0`.

## Unreleased

### Added

- PostgreSQL-first entity schema parsing and deterministic DDL.
- Versioned canonical snapshots, risk-aware diffs, explicit renames, and reversible migration rendering.
- Checksummed migration artifacts with strict manifests, size limits, regular-file checks, and atomic publication.
- Transactional PostgreSQL migration runner with advisory locking, history-prefix validation, apply, status, and rollback.
- Typed immutable select, insert, update, delete, upsert, locking, pagination, returning, batch, and transaction APIs.
- Generated table/column metadata and direct scanners without runtime reflection on query hot paths.
- Explicit one/many relation loading with bounded query counts and no lazy loading.
- `orm` CLI for generate, diff, validate, status, apply, and rollback.
- Versioned strict CLI configuration and deterministic JSON envelopes.
- Observer hooks, structured errors, cancellation, secret redaction, and allocation budgets.
- PostgreSQL 14/18 integration and SQL-injection attack-corpus tests.
- Fuzzing, vulnerability analysis, immutable CI dependencies, and public API compatibility checks.

### Security

- Typed values are always represented as PostgreSQL positional parameters.
- Identifier attacks are rejected or rendered as a single quoted identifier.
- Database URLs and decoded/query-encoded/path-encoded passwords are redacted from CLI errors.
- Upgraded `golang.org/x/text` to `v0.39.0` and `golang.org/x/sync` to `v0.21.0` after vulnerability scanning identified reachable `GO-2026-5970` in the earlier dependency graph.

### Known limits before v1.0.0

- Release artifacts and provenance are not yet published.
- The release candidate has not yet completed the maintained real-application exercise.
- Raw SQL and migration SQL remain trusted developer-authored inputs and must not receive untrusted runtime data.

## Release-note requirements

Every release entry records:

- supported Go and PostgreSQL versions;
- public API, config, snapshot, artifact, or CLI compatibility changes;
- security fixes and dependency upgrades;
- migration behavior changes and manual steps;
- known limitations and recovery guidance;
- release artifact and checksum locations.
