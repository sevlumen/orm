# Security testing and dependency policy

This document defines the continuously enforced security gates for the v1 release line. It complements the operational security boundary documented in `docs/cli.md`.

## SQL-injection guarantee boundary

The typed query and mutation APIs treat application values as PostgreSQL positional parameters. Fuzzing and PostgreSQL 14/18 integration tests assert that quotes, comments, semicolons, tautologies, `UNION`, stacked statements, and delay functions do not change SQL statement shape or execute as SQL.

Generated and validated metadata controls table and column identifiers. Runtime request data must never be used to construct identifiers.

The following remain trusted developer-authored surfaces and are outside the typed-value guarantee:

- raw SQL escape hatches;
- migration `up.sql` and `down.sql`;
- schema type overrides;
- default, generated, check, partial-index, and other SQL expressions.

Do not concatenate request, message, file, environment, or tenant data into trusted SQL surfaces. Parameterization cannot make arbitrary SQL text safe.

## Required pull-request gates

Every pull request runs:

- canonical module-file verification and a readonly dependency graph;
- `go mod verify`;
- `go vet ./...`;
- race-detector tests on the minimum and current stable Go toolchains;
- PostgreSQL integration tests on digest-pinned PostgreSQL 14 and 18 images;
- `govulncheck` including test code;
- deterministic fuzz smoke for parsers and SQL boundaries;
- public API surface comparison against `api/v1.txt`;
- Windows cross-compilation for shipped commands;
- allocation and benchmark smoke tests.

GitHub Actions are referenced by immutable commit SHA. The adjacent comment records the reviewed release tag. PostgreSQL images are referenced by tag and digest so the supported major version remains visible while the exact image is reproducible.

## Fuzz targets

The committed targets cover:

- `entity/FuzzParseTag`;
- `migration/FuzzParseSnapshot`;
- `migration/FuzzRenameValidate`;
- `migration/artifact/FuzzParseManifest`;
- `internal/ormcli/FuzzParseConfig`;
- `postgres/query/FuzzSelectValueRemainsParameterized`;
- `postgres/query/FuzzMetadataValidationNeverPanics`;
- `postgres/runner/FuzzValidateMigrationScript`;
- `postgres/runner/FuzzStatementPrefixesBounded`.

CI runs every target for a short deterministic smoke period. Longer local campaigns should isolate one target per command, for example:

```text
go test ./postgres/runner \
  -run '^$' \
  -fuzz '^FuzzValidateMigrationScript$' \
  -fuzztime=30m
```

Run long campaigns on all supported operating systems before a release candidate when the target crosses platform-specific filesystem or code-generation behavior.

## Fuzz finding handling

A fuzz crash, timeout, excessive allocation, or invariant failure is a release blocker.

1. Preserve the generated corpus entry under the package's `testdata/fuzz/<target>` directory.
2. Minimize and understand the input; never delete it merely to restore CI.
3. Add a named regression test when the security property is easier to understand outside the fuzz harness.
4. Fix the implementation or explicitly narrow the accepted input contract.
5. Re-run the target for an extended period and run the complete race/PostgreSQL matrix.
6. Review whether the finding affects released versions and follow `SECURITY.md` when disclosure is required.

Corpus files must contain only synthetic test data. Never add credentials, production SQL, customer identifiers, access tokens, or private database snapshots.

## Vulnerability analysis

CI runs the pinned `govulncheck` command against packages and tests. A reachable vulnerability is a release blocker. An unreachable or tool-only report still requires documented review before merge.

Triage records should identify:

- affected module and version;
- whether vulnerable symbols are reachable;
- runtime, test-only, generator-only, or CI-only exposure;
- available fixed version or mitigation;
- compatibility and performance impact of the upgrade;
- whether a security advisory or release note is required.

Do not suppress a vulnerability by hiding packages from the scan. A temporary exception must be explicit, narrowly scoped, time bounded, and linked to an issue.

## Dependency changes

Runtime dependencies are intentionally minimal. New runtime dependencies require a design review covering maintenance, transitive graph, licenses, security history, binary size, allocations, and whether the same behavior belongs in generated or standard-library code.

Every dependency update must include:

- an intentional `go.mod` and `go.sum` diff;
- successful `go mod verify` and readonly graph resolution;
- vulnerability analysis;
- full race and PostgreSQL matrices;
- benchmark review when the dependency is on a hot path;
- release-note review when behavior or minimum versions change.

CI and release tools invoked with `go run module@version` must use an exact reviewed version. GitHub Actions must use immutable commit SHAs. Container images used as release evidence must use digests.

## Public API compatibility

`api/v1.txt` is a deterministic source-level manifest of exported declarations. CI fails when the current surface differs.

Regenerate it only after reviewing the compatibility impact:

```text
go run ./cmd/apicheck -write -baseline api/v1.txt
```

Before `v1.0.0`, intentional changes may update the baseline with review. After `v1.0.0`, removals and incompatible signature, exported-field, interface, constant, or type changes require a new major version unless the compatibility policy explicitly permits the change.

The API checker does not replace compile tests or semantic review. Behavioral changes, SQL output changes, config formats, migration artifact formats, and database compatibility require their own tests and policies.

## Secrets and generated artifacts

Tests and diagnostics must not print database URLs or passwords. CLI redaction covers the complete URL plus decoded, query-encoded, and path-encoded password forms. Observer and execution errors preserve structured causes without exposing argument values in public error strings.

Generated code, snapshots, migration artifacts, benchmark output, fuzz corpus, CI logs, release archives, SBOMs, and provenance must be reviewed for secrets before publication. Repository examples use synthetic credentials only.
