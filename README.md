# Sevlumen ORM

PostgreSQL-first, entity-driven ORM and migration tooling for Go.

> Development preview. The current foundation supports validated Go entities, deterministic PostgreSQL DDL, versioned schema snapshots, risk-aware migration diffs, and reversible SQL generation. It is not yet a production-ready ORM runtime.

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

## Generate a migration

Use an empty snapshot for the first migration. Persist the returned snapshot next to the generated SQL and use it as the input for the next migration.

```go
package main

import (
    "fmt"
    "log"

    orm "github.com/sevlumen/orm"
    "github.com/sevlumen/orm/migration"
)

func main() {
    previous := migration.EmptySnapshot()

    generated, next, err := orm.PostgreSQLMigration(previous, User{})
    if err != nil {
        log.Fatal(err)
    }

    if generated.Risk == migration.RiskDestructive {
        log.Fatalf("destructive migration requires manual review: %v", generated.Warnings)
    }

    snapshotJSON, err := next.Marshal()
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println("-- up.sql")
    fmt.Print(generated.Up)
    fmt.Println("-- down.sql")
    fmt.Print(generated.Down)
    fmt.Println("-- snapshot.json")
    fmt.Print(string(snapshotJSON))
}
```

Migration operations are classified as:

- `safe`: straightforward additive changes;
- `review`: changes that can fail depending on existing data, such as `SET NOT NULL` or type conversion;
- `destructive`: dropping a table or column.

Generated rollback SQL recreates schema objects but cannot restore data removed by a destructive migration. Review all generated SQL before applying it. Column/table renames and primary-key or unique-constraint changes are intentionally not guessed; write an explicit migration for those operations.

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

SQL types and default expressions are trusted schema-author input. Never construct them from request data or other untrusted input.

## Road to v1.0

Before a production-ready v1.0 release, the project still needs:

1. migration artifacts, checksums, history, locking, and transactional application;
2. indexes, foreign keys, composite keys, enums, and PostgreSQL-specific schema features;
3. generated typed CRUD/query APIs on top of `pgx`;
4. PostgreSQL integration tests, compatibility guarantees, observability hooks, and release hardening;
5. stable CLI and documented upgrade/rollback procedures.

## Development

```bash
gofmt -w .
go vet ./...
go test -race ./...
```

CI tests the minimum supported Go version and the current stable Go release.

## License

Apache License 2.0. See [LICENSE](LICENSE).
