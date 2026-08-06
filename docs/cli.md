# `orm` command-line interface

The `orm` command orchestrates code generation and the existing checksummed PostgreSQL migration artifact/runner packages. It does not implement a second migration engine and it never loads application entity types through runtime plugins or reflection.

## Exit codes

- `0`: command completed successfully, including `--help`.
- `1`: generation, validation, filesystem, PostgreSQL, checksum, history, lock, or cancellation failure.
- `2`: invalid command usage, unknown flags/config fields, missing confirmation, or conflicting options.

Successful `--json` output uses this stable envelope:

```json
{
  "version": 1,
  "command": "status",
  "result": {}
}
```

Result fields are command-specific. Errors are written to stderr and never include the configured database URL or password.

## Versioned configuration

Every command accepts `--config <path>`. Version 1 uses strict JSON; unknown fields fail rather than being ignored.

```json
{
  "version": 1,
  "generate": {
    "directory": "./internal/data",
    "output": "orm_gen.go",
    "types": ["User", "Order"]
  },
  "migrations": {
    "directory": "./migrations",
    "databaseEnv": "DATABASE_URL",
    "historySchema": "public",
    "historyTable": "__sevlumen_migrations",
    "lockKey": 728472984,
    "maximumRisk": "safe",
    "timeout": "5m"
  }
}
```

Command flags override configuration values. Database credentials should remain in the environment named by `databaseEnv`; do not commit them to the config file. `--database-url` exists for automation that already protects process arguments and logs, but the environment form is preferred.

Configuration files are limited to 1 MiB. Snapshot inputs are limited to 16 MiB. Config, snapshot, artifact, and rename files use strict decoders and reject trailing JSON values.

## Generate typed metadata

```text
orm generate --config orm.json
orm generate --config orm.json --check
```

Flags can replace config values:

```text
orm generate --dir ./models --output orm_gen.go \
  --type User --type Order
```

`--check` never writes and returns exit code 1 when generated code is missing or stale.

## Produce schema snapshots

The CLI intentionally does not load arbitrary application code. Export the current snapshot from a small application-owned Go command using the public API:

```go
snapshot, err := orm.BuildSnapshot(User{}, Order{})
if err != nil {
    return err
}
data, err := snapshot.Marshal()
if err != nil {
    return err
}
return os.WriteFile("schema.snapshot.json", data, 0o644)
```

This keeps entity construction, build tags, and application configuration under the application's compiler and test suite.

## Create migration artifacts

```text
orm diff \
  --config orm.json \
  --after schema.snapshot.json \
  --id 202608060930_add_orders \
  --max-risk review
```

Without `--before`, `diff` uses the snapshot from the latest local migration artifact or an empty snapshot when no artifact exists. An explicit `--before` must match the latest local snapshot; divergent local histories are rejected.

Migration IDs must sort lexicographically after the latest local ID. The command refuses no-op migrations and risk above `--max-risk` or `migrations.maximumRisk`.

Generated artifacts contain:

- `manifest.json`
- `up.sql`
- `down.sql`
- `snapshot.json`

They are written through the existing atomic artifact writer with SHA-256 checksums.

### Explicit renames

Provide a strict versioned rename file:

```json
{
  "version": 1,
  "renames": [
    {"kind": "table", "from": "users", "to": "accounts"},
    {"kind": "column", "table": "accounts", "from": "name", "to": "display_name"}
  ]
}
```

```text
orm diff --after schema.snapshot.json \
  --id 202608061000_rename_accounts \
  --renames renames.json \
  --max-risk review
```

Rename order is significant and rollback reverses that order.

## Validate without a database

Validate every local artifact, checksum, manifest, and embedded snapshot:

```text
orm validate --config orm.json
```

Validate one snapshot instead:

```text
orm validate --snapshot schema.snapshot.json
```

`--snapshot` and `--migrations` cannot be combined.

## Inspect migration status

```text
orm status --config orm.json
orm status --config orm.json --json
```

Status validates local artifacts, creates the configured history table when necessary, acquires the advisory lock, validates that applied history is a prefix of local history, and returns local/applied/pending IDs.

## Apply migrations

The default maximum risk is `safe`:

```text
orm apply --config orm.json
```

Allow review-risk artifacts explicitly:

```text
orm apply --config orm.json --max-risk review
```

Destructive-risk execution requires both the destructive gate and confirmation:

```text
orm apply --config orm.json --max-risk destructive --yes
```

Each artifact is applied in its own PostgreSQL transaction. The runner verifies artifact checksums, advisory lock ownership, history prefix, and artifact risk before executing SQL.

## Roll back migrations

Every rollback requires explicit confirmation:

```text
orm rollback --config orm.json --steps 1 --yes
```

Rollback operates from the latest applied migration backward and validates local checksums against recorded history before executing `down.sql` transactionally. A generated rollback cannot restore data removed by a destructive `up.sql`; review destructive migrations and backups independently.

## Timeouts and cancellation

Database commands accept `--timeout`, overriding `migrations.timeout`:

```text
orm status --config orm.json --timeout 30s
```

The binary converts SIGINT and SIGTERM into context cancellation. Advisory-lock waits, PostgreSQL calls, and transaction operations receive the same bounded context. The runner still uses its independent rollback cleanup context when a transaction must be aborted.

## Secret handling

- Prefer `databaseEnv` over `--database-url`.
- Database URLs and extracted passwords are replaced with `[REDACTED]` in CLI errors.
- JSON success output never contains database connection details.
- PostgreSQL and migration errors may still contain schema names, SQL, or database values supplied by the server. Apply the application's normal log redaction policy when retaining stderr from automated jobs.
