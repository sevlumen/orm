# Explicit schema renames

Schema diffs cannot reliably distinguish a rename from deleting one object and creating another. Sevlumen therefore never guesses rename intent. Use `migration.DiffWithOptions` or `orm.PostgreSQLMigrationWithOptions` to declare renames explicitly.

## Entity migration example

```go
package main

import (
    orm "github.com/sevlumen/orm"
    "github.com/sevlumen/orm/migration"
)

sql, next, err := orm.PostgreSQLMigrationWithOptions(
    previous,
    migration.DiffOptions{Renames: []migration.Rename{
        {
            Kind: migration.RenameTable,
            From: "accounts",
            To:   "customers",
        },
        {
            Kind:  migration.RenameColumn,
            Table: "customers",
            From:  "id",
            To:    "customer_id",
        },
    }},
    Customer{},
)
```

The generated PostgreSQL SQL preserves table data:

```sql
ALTER TABLE "accounts" RENAME TO "customers";
ALTER TABLE "customers" RENAME COLUMN "id" TO "customer_id";
```

`down.sql` reverses the operations in the opposite order.

## Supported rename kinds

| Kind | Required fields | PostgreSQL operation |
|---|---|---|
| `migration.RenameTable` | `From`, `To` | `ALTER TABLE ... RENAME TO ...` |
| `migration.RenameColumn` | `Table`, `From`, `To` | `ALTER TABLE ... RENAME COLUMN ...` |
| `migration.RenameIndex` | `Table`, `From`, `To` | `ALTER INDEX ... RENAME TO ...` |
| `migration.RenameConstraint` | `Table`, `From`, `To` | `ALTER TABLE ... RENAME CONSTRAINT ...` |
| `migration.RenameEnum` | `From`, `To` | `ALTER TYPE ... RENAME TO ...` |

Constraint renames support named primary keys, unique constraints, check constraints, and foreign keys.

## Ordered intent

Rename intents are applied in the order they are declared. Later intents observe earlier names. For example, rename a table before renaming one of its columns:

```go
Renames: []migration.Rename{
    {Kind: migration.RenameTable, From: "accounts", To: "customers"},
    {Kind: migration.RenameColumn, Table: "customers", From: "id", To: "customer_id"},
}
```

The renderer executes renames before newly created enums or tables. This permits a migration to rename an object and then reuse its old name for a new object. Rollback drops the new object before reversing the rename.

## Structured dependency updates

Before normal diff planning, Sevlumen updates structured snapshot references for:

- primary and unique key columns;
- index key and include columns;
- local and referenced foreign-key columns;
- foreign-key referenced table names;
- enum column types and enum arrays;
- named primary, unique, check, and foreign-key constraints.

PostgreSQL itself preserves dependent catalog objects during table and column renames, so generated migrations do not rebuild those objects unnecessarily.

## Raw SQL boundary

Sevlumen never performs textual search-and-replace inside trusted raw SQL expressions:

- generated-column expressions;
- check expressions;
- expression indexes;
- partial-index predicates;
- defaults or custom type expressions.

When a raw expression must change because of a rename, provide an explicit SQL migration for that expression. This avoids corrupting quoted identifiers, string literals, function bodies, or unrelated tokens.

## Validation and risk

Each rename is classified `review` because PostgreSQL acquires catalog or relation locks and application code must be deployed compatibly. Planning fails when:

- a source object does not exist;
- a target already exists;
- a parent table is missing;
- the same source is ambiguous;
- an identifier is empty, contains NUL, or exceeds PostgreSQL's 63-byte boundary;
- ordered intents leave an invalid transformed schema.

Without explicit options, ordinary `migration.Diff` remains conservative and may report an explicit-migration error or produce destructive drop/create operations. Always inspect risk and generated SQL before applying a migration.
