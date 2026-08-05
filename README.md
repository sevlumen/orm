# Sevlumen ORM

PostgreSQL-first, entity-driven ORM and migration tooling for Go.

> Development preview. The project currently provides validated Go entities, deterministic PostgreSQL DDL, versioned schema snapshots, risk-aware diffs, checksummed migration artifacts, and a transactional PostgreSQL migration runner. The typed ORM query runtime is still under development.

## Requirements

- Go 1.25 or newer
- PostgreSQL 14 or newer

## Install

```bash
go get github.com/sevlumen/orm
```

## Entity to PostgreSQL SQL

```go
package main

import (
    "fmt"
    "log"
    "time"

    orm "github.com/sevlumen/orm"
)

type User struct {
    ID          string     `orm:"type:uuid;primaryKey;default:gen_random_uuid()"`
    Email       string     `orm:"type:varchar(320);notNull;unique"`
    DisplayName *string    `orm:"column:display_name;type:varchar(200)"`
    Active      bool       `orm:"notNull;default:true"`
    CreatedAt   time.Time  `orm:"column:created_at;notNull;default:now()"`
    DeletedAt   *time.Time `orm:"column:deleted_at"`
}

func (User) TableName() string { return "users" }

func main() {
    sql, err := orm.PostgreSQLSchema(User{})
    if err != nil {
        log.Fatal(err)
    }
    fmt.Print(sql)
}
```

Output:

```sql
CREATE TABLE "users" (
    "id" uuid NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    "email" varchar(320) NOT NULL UNIQUE,
    "display_name" varchar(200),
    "active" boolean NOT NULL DEFAULT true,
    "created_at" timestamptz NOT NULL DEFAULT now(),
    "deleted_at" timestamptz
);
```

## Generate and persist a migration

Use an empty snapshot for the first migration. The artifact package writes a complete migration directory through a temporary directory and atomic rename, then verifies strict manifests and SHA-256 checksums when loading it.

```go
previous := migration.EmptySnapshot()

generated, next, err := orm.PostgreSQLMigration(previous, User{})
if err != nil {
    log.Fatal(err)
}

bundle, err := artifact.Build(
    "20260805210000_create_users",
    generated,
    next,
)
if err != nil {
    log.Fatal(err)
}

path, err := artifact.Write("migrations", bundle)
if err != nil {
    log.Fatal(err)
}
fmt.Println(path)
```

The generated directory contains:

```text
migrations/
└── 20260805210000_create_users/
    ├── manifest.json
    ├── up.sql
    ├── down.sql
    └── snapshot.json
```

Migration IDs use `YYYYMMDDHHMMSS_lower_snake_case`. `artifact.Load` verifies the manifest, snapshot, regular-file boundaries, size limits, and checksums. `artifact.List` returns migration IDs in deterministic order.

## Apply migrations to PostgreSQL

```go
package main

import (
    "context"
    "log"

    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/sevlumen/orm/migration"
    "github.com/sevlumen/orm/postgres/runner"
)

func main() {
    ctx := context.Background()

    pool, err := pgxpool.New(ctx, "postgres://app:secret@localhost/app")
    if err != nil {
        log.Fatal(err)
    }
    defer pool.Close()

    migrations, err := runner.New(pool, runner.Config{
        MigrationsDir: "migrations",
        MaximumRisk:   migration.RiskReview,
    })
    if err != nil {
        log.Fatal(err)
    }

    status, err := migrations.Status(ctx)
    if err != nil {
        log.Fatal(err)
    }
    log.Printf("migration status: %#v", status)

    applied, err := migrations.Apply(ctx)
    if err != nil {
        log.Fatal(err)
    }
    log.Printf("applied: %#v", applied)
}
```

Runner behavior:

- acquires one PostgreSQL connection and a session-level advisory lock for the complete operation;
- creates the configurable migration history table when status, apply, or rollback is first used;
- verifies that applied history is an exact prefix of the local artifact sequence;
- rejects edited or missing applied migrations through complete artifact checksums;
- preflights every pending migration against `MaximumRisk` before applying the first one;
- executes each migration and its history mutation in one transaction;
- rolls back the latest applied migrations in reverse order;
- rejects transaction-control statements and `COPY` scripts because the runner owns transaction boundaries and does not provide an external COPY data stream.

`MaximumRisk` defaults to `safe`. Set it explicitly to `review` or `destructive` only after reviewing the generated SQL. Generated rollback SQL can restore schema shape but cannot restore data removed by destructive migrations.

## Migration risk levels

- `safe`: straightforward additive changes;
- `review`: changes that can fail depending on existing data, such as `SET NOT NULL`, type conversions, or constrained-column additions;
- `destructive`: dropping a table or column.

Column/table renames and primary-key or unique-constraint changes are intentionally not guessed. Write an explicit migration for operations where intent cannot be inferred safely.

## Entity tags

| Tag | Meaning |
|---|---|
| `column:name` | Override the inferred snake_case column name |
| `type:sql_type` | Override the inferred PostgreSQL type |
| `primaryKey` | Mark the column as the primary key |
| `unique` | Add a unique constraint |
| `notNull` | Force `NOT NULL` |
| `nullable` | Allow `NULL` for a non-pointer field |
| `default:expression` | Add a PostgreSQL default expression |
| `-` | Ignore the field |

Pointers are nullable by default. Non-pointer fields are non-nullable. Supported inferred types currently include strings, booleans, signed integers, floats, `[]byte`, and `time.Time`. Custom Go types can use an explicit `type:` tag.

SQL types, default expressions, and migration SQL are trusted developer-authored inputs. Never construct them from request data or other untrusted input.

## Road to v1.0

Before a production-ready v1.0 release, the project still needs:

1. indexes, foreign keys, composite keys, checks, enums, and PostgreSQL-specific schema features;
2. generated typed CRUD/query APIs on top of `pgx`;
3. stable CLI commands for generate, diff, validate, apply, rollback, and status;
4. observability hooks, fuzzing, vulnerability scanning, release automation, and compatibility policy;
5. release-candidate validation in a real application.

## Development

```bash
gofmt -w .
go mod tidy
git diff --exit-code -- go.mod go.sum
go vet ./...
go test -race ./...
```

CI tests Go 1.25 and the current stable Go release, plus PostgreSQL 14 and 18 integration matrices.

## License

Apache License 2.0. See [LICENSE](LICENSE).
