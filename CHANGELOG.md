# Changelog

All notable changes to Sevlumen ORM are documented here. The project follows semantic versioning beginning with `v1.0.0`.

## Unreleased

### Added

- PostgreSQL-first entity schema parsing and deterministic DDL.
- Versioned canonical snapshots, risk-aware diffs, explicit renames, and reversible migration rendering.
- Checksummed migration artifacts with strict manifests, size limits, regular-file checks, and atomic publication.
- Transactional PostgreSQL migration runner with advisory locking, history-prefix validation, apply, status, rollback, and ambiguous-commit reconciliation.
- Typed immutable select, insert, update, delete, upsert, locking, pagination, returning, batch, and transaction APIs.
- Generated table/column metadata and direct scanners without runtime reflection on query hot paths.
- Explicit one/many relation loading with bounded query counts and no lazy loading.
- `orm` CLI for generate, diff, validate, status, apply, and rollback.
- Versioned strict CLI configuration and deterministic JSON envelopes.
- Observer hooks, structured errors, cancellation, secret redaction, and allocation budgets.
- Native `database/sql` execution backed by `github.com/sevlumen/postgres` without CGo or `libpq`.
- PostgreSQL 14/18 integration, SQL-injection attack-corpus tests, and a maintained end-to-end release-candidate application.
- Fuzzing, vulnerability analysis, immutable CI dependencies, public API compatibility checks, reproducible release archives, checksums, SBOMs, and attestations.

### Security

- Typed values are always represented as PostgreSQL positional parameters.
- Identifier attacks are rejected or rendered as a single quoted identifier.
- Database URLs and decoded/query-encoded/path-encoded passwords are redacted from CLI errors.
- Migration scripts are accepted only as trusted developer-authored artifacts; runtime application values remain on parameterized execution paths.
- Pinned migration sessions are discarded when advisory-lock cleanup or protocol state cannot be trusted.
- Ambiguous commit outcomes are reconciled against checksummed migration history instead of being retried blindly.

### Release-candidate requirements

- The same reviewed commit must pass Go 1.25/current quality and race tests.
- PostgreSQL 14 and PostgreSQL 18 integration and maintained application workflows must pass.
- Generated metadata, public API baselines, documentation links, fuzz smoke, and vulnerability analysis must pass.
- Release artifacts must rebuild byte-for-byte and pass archive, checksum, manifest, executable-metadata, SBOM, and provenance verification.
- Raw SQL and migration SQL remain trusted developer-authored inputs and must not receive untrusted runtime data.

## Release-note requirements

Every release entry records:

- supported Go and PostgreSQL versions;
- public API, config, snapshot, artifact, or CLI compatibility changes;
- security fixes and dependency upgrades;
- migration behavior changes and manual steps;
- known limitations and recovery guidance;
- release artifact and checksum locations.
