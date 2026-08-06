# Security policy

## Supported versions

Security fixes are provided for the latest stable release in the current major version. Before `v1.0.0`, only the latest release candidate is supported. Older prereleases may receive a fix only when the maintainers explicitly say so.

| Version | Security support |
|---|---|
| Latest `v1.x` | Supported |
| Earlier `v1.x` | Upgrade to the latest `v1.x` |
| Prerelease before `v1.0.0` | Latest release candidate only |
| Unreleased source snapshots | Best effort; not a supported production version |

## Reporting a vulnerability

Do not open a public issue for a suspected vulnerability that could expose credentials, enable SQL execution, bypass migration integrity, corrupt data, or compromise release artifacts.

Use GitHub's private security-advisory reporting for this repository. Include:

- affected version or commit;
- operating system, Go version, and PostgreSQL version;
- minimal reproduction or failing test;
- expected and observed behavior;
- impact and realistic attack prerequisites;
- whether secrets or production data were involved;
- any proposed mitigation.

Do not include real credentials, customer data, production snapshots, or private infrastructure details. Replace them with synthetic values.

## Response process

Maintainers will:

1. acknowledge a complete report;
2. reproduce and classify the issue;
3. determine affected releases and coordinated-disclosure needs;
4. develop regression tests and a fix on a private advisory branch when appropriate;
5. run vulnerability, fuzz, race, PostgreSQL, API-compatibility, and release-artifact gates;
6. publish a fixed release and advisory;
7. credit the reporter unless anonymity was requested.

Response timing depends on severity and reproducibility. No fixed remediation deadline is promised, but confirmed critical vulnerabilities block releases and receive priority over feature work.

## Security boundaries

Typed query and mutation values are sent as PostgreSQL positional parameters. Generated or validated table and column metadata is quoted as PostgreSQL identifiers. Regression and fuzz tests cover quotes, comments, semicolons, tautologies, unions, stacked statements, delay functions, and identifier attacks.

The following are trusted developer-authored SQL surfaces and are not made safe by parameterization:

- `TrustedSQL` and other raw-SQL escape hatches;
- migration `up.sql` and `down.sql`;
- schema type overrides;
- default, generated, check, index-expression, and predicate expressions.

Never concatenate HTTP, message, file, tenant, environment, or other untrusted runtime input into trusted SQL surfaces.

Migration checksums, regular-file checks, risk gates, advisory locks, and transactions protect artifact integrity and execution order. They do not validate arbitrary SQL as harmless and do not restore data removed by destructive migrations.

See [Security testing](docs/security-testing.md), [CLI security boundary](docs/cli.md#sql-injection-boundary), and [Recovery runbook](docs/recovery.md).

## Scope

Reports are in scope when they affect this repository's code, published binaries, generated artifacts, documented release workflow, or supported dependency graph. Vulnerabilities in PostgreSQL, Go, GitHub, or other external infrastructure should also be reported upstream; explain the ORM-specific exposure when reporting here.

Denial of service requiring an intentionally unbounded trusted migration file, social engineering, and attacks that already require arbitrary code execution in the application process may be classified as outside the library's threat boundary. They are still welcome as hardening reports when a practical defense exists.
