# Typed metadata generation

`ormgen` creates typed `query.Table`, `query.Column`, and direct row-scanner declarations from Go entity structs. Generated scanners call `row.Scan(&value.Field, ...)` directly and do not use reflection.

## Configure generation

Add a `go:generate` directive in the entity package:

```go
//go:generate go run github.com/sevlumen/orm/cmd/ormgen \
//  -type User -type Order -output orm_gen.go
```

Run generation from the package directory:

```text
go generate ./...
```

Multiple type names may also be comma-separated:

```text
ormgen -type User,Order -output orm_gen.go
```

The output path must be a `.go` file inside the selected package directory. The output file is excluded while parsing so repeated generation is deterministic.

## Table names

The generator resolves table names in this order:

1. `// orm:table <name>` on the type declaration.
2. A `TableName() string` method that returns exactly one string literal.
3. Deterministic snake case of the type name.

```go
// orm:table application_users
type User struct {
    ID int64
}
```

A computed `TableName()` implementation is rejected because executing application code during generation would make the output environment-dependent. Use the directive for dynamic application naming logic.

## Columns and scanners

Exported fields become selectable columns in declaration order. The standard `orm` tag controls the column name, and `orm:"-"` excludes a field:

```go
type User struct {
    ID        int64          `orm:"column:user_id;primaryKey;insertOnly"`
    Email     string         `orm:"unique"`
    Payload   map[string]any `orm:"type:jsonb"`
    Search    string         `orm:"generated:lower(email);readOnly"`
    Transient string         `orm:"-"`
}
```

Generated metadata is exposed as `<Type>ORM`:

```go
statement, err := query.Select(UserORM.Table).
    Where(UserORM.Email.Eq("person@example.com")).
    Build()
```

Imported field types are preserved in generic column declarations. Explicit import aliases are recommended when the package name differs from the final path segment.

## Mutation capabilities

The generator recognizes these query-only tag options:

- `readOnly`: selectable but not insertable or updatable.
- `insertOnly`: insertable but not updatable.
- `updateOnly`: updatable but not insertable.

A generated column is read-only automatically. Capability tags are mutually exclusive. The runtime entity/schema parser accepts the same tags but ignores them for DDL because they describe query mutation behavior rather than database structure.

## Validation boundaries

Generation fails with file and line diagnostics for:

- missing, unexported, non-struct, or generic entity types;
- embedded fields and declarations containing multiple field names;
- tagged unexported fields;
- duplicate column names;
- malformed or conflicting tags;
- unsupported computed `TableName()` methods;
- generated declaration conflicts;
- the reserved entity field name `Table`, which conflicts with the metadata bundle field;
- unresolved imported type aliases.

Generated metadata is initialized through generated fail-fast helpers. All table and column literals are validated before code is emitted; an initialization panic therefore indicates stale generated code or a generator/runtime version mismatch and should fail application startup immediately.

## Stale-output checks

CI can verify committed generated files without modifying the workspace:

```text
go run github.com/sevlumen/orm/cmd/ormgen \
  -type User -type Order -output orm_gen.go -check
```

Check mode returns a `generator.StaleError` when the output is missing or differs. It never writes files.

Renaming or removing an entity field leaves the old scanner referencing the previous Go field, so stale generated code fails compilation. Regenerate after entity changes and commit the updated output.

## Atomicity and dependency footprint

`ormgen` parses source using the Go standard library only. It does not add `go/packages`, compiler plugins, or runtime reflection dependencies.

Generation formats the complete result before touching the output. Normal writes use a temporary file, sync it, and atomically rename it over the destination. Parse, validation, or formatting failures preserve the existing generated file.
