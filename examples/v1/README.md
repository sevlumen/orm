# v1 API example

This package is compiled and tested by the repository's ordinary Go and race-test matrices. It demonstrates:

- the entity tags and metadata shape produced by `orm generate` / `ormgen`;
- parameterized insert, select, update, and delete builders;
- explicit transactions through `InTransaction`;
- explicit atomic batch execution through `database/sql`;
- explicit many-relation loading with a bounded chunk size;
- dependency-free observer hooks;
- safe event summaries that never include query arguments.

Generate metadata in an application package with either command:

```text
orm generate --dir ./internal/data --output orm_gen.go --type User --type Order
ormgen -dir ./internal/data -output orm_gen.go -type User -type Order
```

Commit generated output and enforce stale checking in CI:

```text
orm generate --dir ./internal/data --output orm_gen.go \
  --type User --type Order --check
```

The manual metadata in `example.go` mirrors generated output so the complete query surface remains readable in one file. The maintained release-candidate application exercises the real generator and PostgreSQL workflow end to end.

Run:

```text
go test ./examples/v1
```

The SQL-injection test intentionally supplies a stacked-statement payload and verifies that it remains in PostgreSQL arguments rather than SQL text.
