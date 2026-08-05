# Sevlumen ORM

PostgreSQL-first, entity-driven ORM and migration tooling for Go.

> Early MVP: the current API converts Go entity structs into deterministic PostgreSQL `CREATE TABLE` SQL. Schema snapshots, migration diffs, relations and typed queries are planned next.

## Install

```bash
go get github.com/sevlumen/orm
```

## Quick start

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

## Design direction

The project is intentionally PostgreSQL-first and code-generation-friendly. Planned layers:

1. Entity metadata and schema validation.
2. Snapshot-based migration diff and SQL files.
3. PostgreSQL migration history and drift detection.
4. Generated typed CRUD/query APIs on top of `pgx`.
5. Optional tracking sessions and `SaveChanges` semantics.

## Development

```bash
gofmt -w .
go vet ./...
go test -race ./...
```

## License

Apache License 2.0. See [LICENSE](LICENSE).
