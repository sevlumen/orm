# Typed database/sql executor

The `postgres/query` executor runs immutable typed builders through the standard `database/sql` execution contracts. `*sql.DB`, `*sql.Conn`, and `*sql.Tx` satisfy the runtime interfaces. Generated table scan functions remain the only row-mapping path; executor helpers do not use reflection while scanning rows.

## Create an executor

```go
executor, err := query.NewExecutor(database)
if err != nil {
    return err
}
```

An executor is safe for concurrent reuse when its database implementation and observer are concurrency-safe. `*sql.DB` is the normal application-level implementation. A transaction-bound executor must remain inside its transaction callback.

Typed-nil databases, observers, transaction beginners, and transactions are rejected instead of failing later with a panic.

## Select rows

```go
users, err := query.FetchAll(ctx, executor,
    query.Select(Users.Table).
        Where(Users.Active.Eq(true)).
        OrderBy(Users.ID.Asc()),
)

user, err := query.FetchOne(ctx, executor,
    query.Select(Users.Table).Where(Users.ID.Eq(id)),
)

user, found, err := query.FetchOptional(ctx, executor,
    query.Select(Users.Table).Where(Users.Email.Eq(email)),
)
```

`FetchOne` and `FetchOptional` apply `LIMIT 1`. `FetchOne` returns an error wrapping `sql.ErrNoRows`; `FetchOptional` returns `found=false` without an error. Every multi-row path closes `*sql.Rows` and checks `Rows.Err()`.

## Mutations

Non-returning helpers return a driver-neutral `query.Result`:

```go
result, err := query.ExecUpdate(ctx, executor,
    query.Update(Users.Table).
        Set(Users.Active.Set(false)).
        Where(Users.ID.Eq(id)),
)
rowsAffected := result.RowsAffected
```

Returning helpers are available in exactly-one and all-row forms:

```go
created, err := query.InsertOne(ctx, executor,
    query.Insert(Users.Table).Row(
        Users.ID.Set(id),
        Users.Email.Set(email),
        Users.Active.Set(true),
    ),
)

updated, err := query.UpdateAll(ctx, executor,
    query.Update(Users.Table).
        Set(Users.Active.Set(false)).
        Where(Users.TeamID.Eq(teamID)),
)
```

Available pairs are `InsertOne` / `InsertAll`, `UpdateOne` / `UpdateAll`, and `DeleteOne` / `DeleteAll`.

`InsertOne` rejects a multi-row input before execution. `UpdateOne` and `DeleteOne` detect multiple returned rows and return an error wrapping `query.ErrMultipleRows`. PostgreSQL has already performed the mutation when cardinality is detected, so use `InTransaction` when the caller needs a cardinality error to roll the operation back.

## Transactions

```go
err := query.InTransaction(
    ctx,
    database,
    query.TxOptions{Isolation: query.IsolationSerializable},
    func(txExecutor *query.Executor) error {
        if _, err := query.ExecUpdate(ctx, txExecutor, update); err != nil {
            return err
        }
        _, err := query.ExecInsert(ctx, txExecutor, auditInsert)
        return err
    },
)
```

Callback errors roll the transaction back. Callback panics also trigger rollback and are then rethrown. Commit errors are returned. Rollback uses a bounded context independent of the canceled request context so cleanup still has an opportunity to complete.

`TransactionError` exposes the failing stage through `Stage` and preserves the cause through `errors.Is`, `errors.As`, and `Unwrap`.

## Batches

Mutation batches are immutable:

```go
batch := query.QueueInsert(query.NewBatch(), firstInsert)
batch = query.QueueUpdate(batch, secondUpdate)
results, err := query.ExecBatch(ctx, executor, batch)
```

Returning builders are rejected in mutation batches. `FetchBatch` accepts homogeneous typed `SELECT` builders and returns one result slice per builder.

When the executor is backed by `*sql.DB` or `*sql.Conn`, queued mutations execute sequentially inside one explicit transaction. When it is already backed by `*sql.Tx`, the caller owns the surrounding transaction. The v1 implementation favors deterministic atomicity over hidden protocol pipelining.

## Cancellation

All operations use the supplied context. A timeout or cancellation is returned as an `ExecutionError` cause. Rows are closed before returning so a healthy pooled connection can be reused. The Sevlumen PostgreSQL driver discards a backend when protocol state cannot be trusted.

## Observability and error privacy

```go
type Observer interface {
    Before(context.Context, query.Event)
    After(context.Context, query.Event)
}
```

Observers may be called concurrently. Observer panics are recovered and ignored so instrumentation cannot change query semantics.

`Event` contains operation name, parameterized SQL, timing, rows affected, and a structured error. Statement argument values are never attached to events.

`ExecutionError.Error()` contains the operation and SQL only. It intentionally omits raw driver error text because PostgreSQL errors can contain database values. The original cause remains available through `errors.Is`, `errors.As`, and `Unwrap`; applications should apply their own redaction policy before logging underlying driver errors.

SQL itself can contain sensitive content when `TrustedSQL` is misused. Never build trusted SQL from request, tenant, or user data.
