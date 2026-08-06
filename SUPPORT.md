# Support policy

## Supported environments

The v1 release line supports:

- Go 1.25 or newer within the current Go 1 compatibility promise;
- PostgreSQL 14 through 18;
- Linux, macOS, and Windows for the `orm` and `ormgen` commands;
- PostgreSQL connections provided by `pgx/v5`.

CI continuously tests Go 1.25, the current stable Go toolchain, PostgreSQL 14, and PostgreSQL 18. Intermediate supported PostgreSQL majors are expected to follow the same SQL contract; release-candidate validation must exercise any major with a known behavioral difference.

## What support means

For the latest stable `v1.x` release, maintainers accept reproducible reports for:

- incorrect SQL generation;
- typed query or mutation behavior;
- code generation and stale-code detection;
- migration diff, artifact, apply, status, and rollback behavior;
- documented compatibility regressions;
- security, integrity, cancellation, and concurrency defects;
- release binary or checksum problems.

Feature requests and unsupported-database requests are considered separately from defects and do not imply a delivery commitment.

## Required issue information

Provide:

- ORM version or commit;
- Go and PostgreSQL versions;
- operating system and architecture;
- minimal entities, snapshot, migration, or query builder code;
- generated SQL with secret values removed;
- expected and observed behavior;
- a minimal repository or test when practical.

Never post database URLs, passwords, tokens, customer data, or production snapshots. Use [private security reporting](SECURITY.md) for potential vulnerabilities.

## Compatibility and deprecation

Public Go APIs, CLI JSON/config version 1, and migration artifact format version 1 follow [the compatibility policy](docs/compatibility.md). Deprecations remain available for at least one minor release and are documented in the changelog before removal in a future major version.

## Unsupported configurations

The following are outside the v1 support contract:

- databases other than PostgreSQL;
- Go versions older than 1.25;
- PostgreSQL versions older than 14;
- modified applied migration artifacts;
- arbitrary request-derived raw SQL or identifiers;
- automatic schema mutation during application startup without an explicit deployment review;
- lazy loading, hidden N+1 behavior, and reflection-based runtime entity discovery;
- restoring data deleted by destructive migrations.

Best-effort discussion may still occur, but unsupported configurations do not block releases.
