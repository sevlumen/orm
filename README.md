# Sevlumen ORM

PostgreSQL-first, entity-driven ORM, code generator, and migration tooling for Go.

> Release-candidate hardening. The typed runtime, generated metadata, migration system, CLI, security gates, reproducible artifacts, and maintained real-application exercise are implemented. A final `v1.0.0` tag requires the same reviewed candidate commit to pass every quality, PostgreSQL 14/18, release-reproducibility, and RC-application gate.

## Requirements

- Go 1.25 or newer.
- PostgreSQL 14 through 18.

CI tests Go 1.25, the current stable Go release, PostgreSQL 14, and PostgreSQL 18. See [Support policy](SUPPORT.md) and [Compatibility policy](docs/compatibility.md).

## Install

```text
go get github.com/sevlumen/orm
```

The repository ships two commands:

```text
go install github.com/sevlumen/orm/cmd/orm@latest
go install github.com/sevlumen/orm/cmd/ormgen@latest
```

Use an immutable release version instead of `@latest` in production automation after `v1.0.0` is published.

## Design contract

Sevlumen ORM is intentionally explicit:

- PostgreSQL-first execution through `database/sql`, backed by `github.com/sevlumen/postgres`; no `pgx`, CGo, or `libpq` dependency.
- Generated table/column metadata and direct scanners; no reflection on query hot paths.
- Immutable typed builders for select, insert, update, delete, upsert, pagination, locking, and `RETURNING`.
- Explicit transaction and batch APIs.
- Explicit one/many relation loading with bounded query counts; no lazy loading or hidden N+1 queries.
- Versioned canonical snapshots and reviewed migration SQL.
- Checksummed artifacts, advisory locking, transactional apply/rollback, and exact history-prefix validation.
- Raw SQL escape hatches remain available but are visibly trusted developer input.

## SQL-injection boundary

Typed query and mutation values are PostgreSQL positional parameters. Attack-corpus and fuzz tests verify that quotes, comments, semicolons, tautologies, `UNION`, stacked statements, and delay functions do not alter SQL shape or execute as SQL. Generated/validated identifiers are quoted or rejected fail-closed.

Raw SQL, `TrustedSQL`, migration SQL, type overrides, defaults, checks, generated expressions, and expression indexes are developer-authored SQL. Never concatenate request, message, file, tenant, or other untrusted runtime input into those surfaces.

See [Security policy](SECURITY.md), [Security testing](docs/security-testing.md), and [CLI SQL boundary](docs/cli.md#sql-injection-boundary).

## Entity and schema

```go
package data

import "time"

type User struct {
    ID        string    `orm:"type:uuid;primaryKey;default:gen_random_uuid();insertOnly"`
    Email     string    `orm:"type:varchar(320);notNull;unique"`
    Active    bool      `orm:"notNull;default:true"`
    CreatedAt time.Time `orm:"column:created_at;notNull;default:now();readOnly"`
}

func (User) TableName() string { return "users" }
```

Generate typed metadata and a direct row scanner:

```text
orm generate --dir ./internal/data --output orm_gen.go --type User
orm generate --dir ./internal/data --output orm_gen.go --type User --check
```

`--check` fails when generated output is missing or stale. Commit generated code beside the entity source.

Build deterministic PostgreSQL schema SQL:

```go
sql, err := orm.PostgreSQLSchema(User{})
```

Advanced schema configuration supports extensions, enums, composite keys, unique/check/foreign-key constraints, deferrability, ordinary/expression/partial indexes, include columns, and PostgreSQL index methods.

## Typed query and mutation API

Generated metadata exposes a typed table and columns:

```go
statement, err := query.Select(data.UserORM.Table).
    Where(data.UserORM.Email.Eq(email)).
    OrderBy(data.UserORM.ID.Asc()).
    Limit(1).
    Build()
```

The SQL contains placeholders; `email` remains in `statement.Args`.

Execute through one reusable executor:

```go
executor, err := query.NewExecutor(database, query.WithObserver(observer))
user, found, err := query.FetchOptional(ctx, executor,
    query.Select(data.UserORM.Table).
        Where(data.UserORM.Email.Eq(email)).
        Limit(1),
)
```

Returning mutation:

```go
created, err := query.InsertOne(ctx, executor,
    query.Insert(data.UserORM.Table).
        Row(
            data.UserORM.Email.Set(email),
            data.UserORM.Active.Set(true),
        ).
        Returning(),
)
```

Transactions, batches, relation loading, observer hooks, and an injection regression example are compile-tested in [the v1 example package](examples/v1/README.md).

## Migration workflow

Export the application snapshot through compiled application code, then create a reviewed artifact:

```text
orm diff \
  --config orm.json \
  --after schema.snapshot.json \
  --id 20260806093000_add_orders \
  --max-risk review
```

Artifacts contain:

```text
migrations/
└── 20260806093000_add_orders/
    ├── manifest.json
    ├── up.sql
    ├── down.sql
    └── snapshot.json
```

Migration IDs use `YYYYMMDDHHMMSS_lower_snake_case`. Artifacts use strict manifests, canonical snapshots, SHA-256 checksums, regular-file and size boundaries, and atomic publication.

Validate without connecting to PostgreSQL:

```text
orm validate --config orm.json
```

Inspect and apply:

```text
orm status --config orm.json --json
orm apply --config orm.json
orm apply --config orm.json --max-risk review
orm apply --config orm.json --max-risk destructive --yes
```

Every rollback requires confirmation:

```text
orm rollback --config orm.json --steps 1 --yes
```

Each artifact executes in its own PostgreSQL transaction. The runner holds one advisory lock for the operation, preflights all pending risk before applying the first migration, and verifies applied history is the exact prefix of local artifacts.

Generated rollback SQL can restore reversible schema shape. It cannot reconstruct values deleted by destructive migrations. Follow the [Recovery runbook](docs/recovery.md).

## Risk levels

- `safe`: straightforward additive operations.
- `review`: operations dependent on existing data, locking, rewriting, or compatibility.
- `destructive`: operations such as dropping a table or column.

Risk flags are gates, not approvals. Review SQL, application compatibility, lock impact, backups, and recovery before production execution.

## CLI configuration and automation

The CLI accepts strict versioned JSON configuration. Flags override config. Automation should use `--json` and the versioned envelope rather than parse human output.

Database URLs and decoded/query-encoded/path-encoded passwords are redacted from CLI error output. Prefer an environment variable over `--database-url`, because operating systems may expose process arguments before application redaction.

See [CLI reference](docs/cli.md).

## Observability and performance

Observer hooks expose operation name, parameterized SQL, timing, affected rows, and structured errors without a mandatory logging/tracing dependency. Query arguments are intentionally absent. See [Observability and redaction](docs/observability.md).

Benchmarks compare builder/scanner overhead and end-to-end typed execution with direct `database/sql` calls backed by `sevlumen/postgres`. CI enforces allocation budgets and runs fixed-iteration smoke benchmarks without claiming noisy hosted-runner latency ratios. See [Benchmark methodology](docs/benchmarks.md).

## Compatibility and operations

- [Compatibility policy](docs/compatibility.md)
- [Upgrade guide](docs/upgrade.md)
- [Recovery runbook](docs/recovery.md)
- [Release-candidate evidence](docs/rc-evidence.md)
- [Support policy](SUPPORT.md)
- [Security policy](SECURITY.md)
- [Changelog](CHANGELOG.md)
- [Contributing](CONTRIBUTING.md)
- [Code of conduct](CODE_OF_CONDUCT.md)

## Development

```text
go mod tidy
git diff --exit-code -- go.mod go.sum
go mod verify
gofmt -w .
go vet ./...
go test -race ./... -count=1
go run ./cmd/doccheck
```

PostgreSQL integration tests:

```text
SEVLUMEN_TEST_DATABASE_URL='postgres://postgres:postgres@127.0.0.1:5432/sevlumen_test?sslmode=disable' \
go test -race ./postgres/... -count=1
```

Maintained release-candidate application:

```text
go build -trimpath -o /tmp/sevlumen-orm ./cmd/orm
SEVLUMEN_RC_ORM_BINARY=/tmp/sevlumen-orm \
SEVLUMEN_TEST_DATABASE_URL='postgres://postgres:postgres@127.0.0.1:5432/sevlumen_test?sslmode=disable' \
go test -race ./examples/rcapp -run '^TestReleaseCandidateWorkflow$' -count=1 -v
```

Security CI also runs vulnerability analysis, deterministic fuzz smoke, immutable dependency checks, and the reviewed public API baseline under `api/v1/`.

## License

Apache License 2.0. See [LICENSE](LICENSE).
