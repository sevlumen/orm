# PostgreSQL-native schema features

Sevlumen models PostgreSQL extensions, enum types, generated columns, JSONB, and arrays as versioned schema metadata. These objects participate in snapshots, migration risk analysis, SQL generation, and rollback ordering.

## Schema-level configuration

Use `entity.ParseWithSchema` when parsed entities require PostgreSQL extensions or enum types.

```go
package model

import "github.com/sevlumen/orm/entity"

model, err := entity.ParseWithSchema(func(schema *entity.SchemaBuilder) {
    schema.Extension("pgcrypto")
    schema.Enum("order_status", "new", "paid", "cancelled")
}, Order{})
```

Extensions are created with `CREATE EXTENSION IF NOT EXISTS`. Generated rollback SQL deliberately retains an added extension because it may have existed before the migration or may be shared by other applications.

Enums are created before tables and dropped only after dependent tables and columns are removed. Enum labels are SQL-quoted by the renderer.

## Entity type inference

The entity parser infers these PostgreSQL types:

| Go type | PostgreSQL type |
|---|---|
| `json.RawMessage` | `jsonb` |
| `map[string]T` | `jsonb` |
| `[]string` | `text[]` |
| `[]bool` | `boolean[]` |
| `[]int8`, `[]int16` | `smallint[]` |
| `[]int`, `[]int32` | `integer[]` |
| `[]int64` | `bigint[]` |
| `[]float32` | `real[]` |
| `[]float64` | `double precision[]` |
| `[]time.Time` | `timestamptz[]` |
| `[][]byte` | `bytea[]` |
| `[]byte` | `bytea` |

Unsupported maps, nested arrays, structs, or application-specific types require an explicit `orm:"type:..."` tag. Explicit enum columns use the enum database type name:

```go
type Order struct {
    Status string `orm:"type:order_status"`
}
```

## Stored generated columns

Use `generated:` for a trusted developer-authored PostgreSQL expression:

```go
type LineItem struct {
    Quantity  int
    UnitPrice int
    Total     int `orm:"generated:quantity * unit_price"`
}
```

The generated SQL is:

```sql
"total" integer GENERATED ALWAYS AS (quantity * unit_price) STORED NOT NULL
```

A generated column cannot also declare a default. Changing an existing generated expression is not inferred because PostgreSQL requires a drop/recreate operation with application-specific data and locking considerations. Write an explicit migration for that change.

## Migration safety

Generated migration behavior is intentionally conservative:

- adding an extension is `review`; rollback keeps it;
- removing an extension requires an explicit migration;
- creating an enum is reversible;
- dropping an unused enum is `destructive` and reversible while the migration remains unapplied;
- changing enum labels or label order requires an explicit migration;
- removing an enum while an after-schema column still uses it is rejected;
- adding a generated column is `review` because existing rows are computed;
- changing a generated expression requires an explicit migration.

Native objects are ordered around relational objects:

1. create extensions and enum types;
2. apply table, column, index, constraint, and foreign-key changes;
3. drop enum types no longer used.

Generated `down.sql` reverses that dependency order.

## Trusted SQL boundary

Generated expressions, explicit column types, defaults, check expressions, index expressions, predicates, and migration SQL are trusted developer-authored input. Never compose them from HTTP parameters, tenant values, or other untrusted data.
