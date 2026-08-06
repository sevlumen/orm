# Typed PostgreSQL query builders

The `postgres/query` package builds immutable PostgreSQL statements from generated typed table and column metadata. It does not execute SQL; execution, scanning, transactions, batching, and generated metadata are layered on top of this core.

## Metadata

```go
package model

import "github.com/sevlumen/orm/postgres/query"

type User struct {
    ID     int64
    Email  string
    Active bool
}

var Users, _ = query.NewTable[User](
    "users",
    []string{"id", "email", "active"},
    func(row query.RowScanner) (User, error) {
        var value User
        err := row.Scan(&value.ID, &value.Email, &value.Active)
        return value, err
    },
)

var UserID, _ = query.NewColumn[User, int64](
    Users,
    "id",
    query.InsertOnlyColumn(),
)

var UserEmail, _ = query.NewColumn[User, string](Users, "email")
var UserActive, _ = query.NewColumn[User, bool](Users, "active")
```

Table and column names are validated against PostgreSQL's 63-byte identifier boundary and are always quoted by the renderer. Table identity is retained in every predicate, assignment, order expression, and conflict target; mixing metadata from two table instances fails during `Build`.

Generated and read-only columns should use `query.ReadOnlyColumn()`. Insert-only identity columns can use `query.InsertOnlyColumn()`.

## SELECT

```go
statement, err := query.Select(Users).
    Where(query.And(
        UserActive.Eq(true),
        UserID.In(10, 20, 30),
    )).
    OrderBy(UserID.Desc()).
    Limit(50).
    Offset(100).
    ForUpdate().
    SkipLocked().
    Build()
```

Result:

```sql
SELECT "id", "email", "active"
FROM "users"
WHERE ("active" = $1) AND ("id" IN ($2, $3, $4))
ORDER BY "id" DESC
LIMIT $5 OFFSET $6
FOR UPDATE SKIP LOCKED
```

All runtime values, including pagination values, are positional arguments. Supported row locks are `FOR UPDATE` and `FOR SHARE`, optionally combined with `SKIP LOCKED` or `NOWAIT`.

## INSERT and UPSERT

```go
statement, err := query.Insert(Users).
    Row(
        UserID.Set(42),
        UserEmail.Set("user@example.com"),
        UserActive.Set(true),
    ).
    OnConflict(UserID.ConflictTarget()).
    DoUpdate(
        UserEmail.Excluded(),
        UserActive.Set(true),
    ).
    Returning().
    Build()
```

Multi-row inserts are supported by calling `Row` repeatedly. Every row must assign the same column set. Column order is canonicalized so generated SQL is deterministic even when callers pass assignments in different orders.

`OnAnyConflict().DoNothing()` emits targetless `ON CONFLICT DO NOTHING`. Targetless conflict handling cannot use `DO UPDATE`.

## UPDATE and DELETE safety

```go
update, err := query.Update(Users).
    Set(UserActive.Set(false)).
    Where(UserID.Eq(42)).
    Returning().
    Build()

deleteStatement, err := query.Delete(Users).
    Where(UserID.Eq(42)).
    Build()
```

`UPDATE` and `DELETE` fail unless they contain a `WHERE` predicate or the caller explicitly opts into `AllRows()`:

```go
statement, err := query.Delete(Users).AllRows().Build()
```

Combining `Where` with `AllRows` is rejected to keep intent unambiguous.

## Parameterization boundary

Values passed through typed predicates and assignments always become `$n` arguments. Injection-shaped strings are never interpolated into SQL.

Developer-authored expressions require the explicit `query.TrustedSQL` type:

```go
query.Select(Users).Where(
    query.TrustedPredicate(
        Users,
        query.TrustedSQL(`lower("email") = current_user`),
    ),
)

query.Update(Users).Set(
    UserEmail.SetSQL(query.TrustedSQL(`lower("email")`)),
)
```

`TrustedSQL` must never be constructed from HTTP parameters, tenant data, user input, or other untrusted values.

## Immutability

Every builder method returns a new value. Existing builders, predicates, assignments, and metadata are not mutated. A prepared base builder can be safely reused concurrently to derive multiple statements; the race-test suite verifies this behavior.
