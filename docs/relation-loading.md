# Explicit typed relation loading

Sevlumen ORM does not implement lazy loading. Reading an entity field never triggers a database query. Relations are loaded only when application code explicitly calls a typed relation loader.

## One-to-many

Create a reusable loader from source and target key extractors plus a typed query factory:

```go
ordersByAccount, err := query.NewManyRelation(
    "account orders",
    query.RequiredKey(func(account Account) int64 { return account.ID }),
    query.RequiredKey(func(order Order) int64 { return order.AccountID }),
    query.SelectRelationByColumn(
        query.Select(OrderORM.Table).
            Where(OrderORM.Active.Eq(true)).
            OrderBy(OrderORM.ID.Asc()),
        OrderORM.AccountID,
    ),
)
if err != nil {
    return err
}

loaded, err := ordersByAccount.Load(ctx, executor, accounts)
```

The returned slice has exactly the same length and order as `accounts`. Each element is:

```go
type ManyResult[T any] struct {
    Values     []T
    KeyPresent bool
}
```

`KeyPresent=false` means the source key extractor reported a NULL or absent key. `KeyPresent=true` with an empty, non-nil `Values` slice means the key exists but no target rows were found.

Repeated source keys are queried once. Every source row receives an independent result slice, so appending to or replacing one result slice does not mutate another source row's relation result.

## Belongs-to and one-to-one

```go
managerByAccount, err := query.NewOneRelation(
    "account manager",
    func(account Account) (int64, bool) {
        if account.ManagerID == nil {
            return 0, false
        }
        return *account.ManagerID, true
    },
    query.RequiredKey(func(manager Account) int64 { return manager.ID }),
    query.SelectRelationByColumn(
        query.Select(AccountORM.Table),
        AccountORM.ID,
    ),
)

loaded, err := managerByAccount.Load(ctx, executor, accounts)
```

One-relation results distinguish all states explicitly:

```go
type OneResult[T any] struct {
    Value      T
    KeyPresent bool
    Found      bool
}
```

- `KeyPresent=false`: source relation key is NULL or absent.
- `KeyPresent=true, Found=false`: source key exists, but no target row exists.
- `KeyPresent=true, Found=true`: `Value` contains the target row.

A one-relation fails with an error wrapping `query.ErrRelationMultipleRows` when more than one target row maps to a requested key. Use a database unique constraint as the primary integrity guarantee; the loader check protects applications from inconsistent schemas or queries.

## Query count and chunking

Source keys are deduplicated in first-seen order. The default chunk size is 1,000 unique keys:

- Empty source list: zero queries.
- Every source key absent: zero queries.
- Up to 1,000 unique keys: one query.
- More keys: one query per chunk.

Configure a smaller or larger chunk explicitly:

```go
loader, err := query.NewManyRelation(
    "account orders",
    sourceKey,
    targetKey,
    relationQuery,
    query.WithRelationChunkSize(500),
)
```

The configured value is the number of relation keys, not necessarily the number of PostgreSQL parameters. A composite-key query may bind several parameters per key, so choose a chunk size that keeps the complete statement below PostgreSQL's parameter limit. The loader rejects chunk sizes greater than 65,535.

Global `LIMIT` and `OFFSET` are rejected in relation queries because they limit the entire relation query, not each source row. Apply relation-specific pagination through an explicit application query rather than relying on relation loading.

## Composite keys

A `RelationQuery` receives the non-empty typed key chunk. Build a fully parameterized predicate using normal typed query expressions:

```go
type AccountKey struct {
    TenantID  int64
    AccountID int64
}

settingsByAccount, err := query.NewOneRelation(
    "account setting",
    query.RequiredKey(func(account Account) AccountKey {
        return AccountKey{TenantID: account.TenantID, AccountID: account.ID}
    }),
    query.RequiredKey(func(setting Setting) AccountKey {
        return AccountKey{TenantID: setting.TenantID, AccountID: setting.AccountID}
    }),
    func(keys []AccountKey) query.SelectBuilder[Setting] {
        predicates := make([]query.Predicate[Setting], len(keys))
        for index, key := range keys {
            predicates[index] = query.And(
                SettingORM.TenantID.Eq(key.TenantID),
                SettingORM.AccountID.Eq(key.AccountID),
            )
        }
        return query.Select(SettingORM.Table).Where(query.Or(predicates...))
    },
)
```

The factory receives a copied key slice. Runtime values still become positional parameters through the normal query builder.

## Filtering and ordering

Additional target filters and ordering belong in the base typed `SELECT`:

```go
query.SelectRelationByColumn(
    query.Select(OrderORM.Table).
        Where(OrderORM.DeletedAt.IsNull()).
        OrderBy(OrderORM.CreatedAt.Desc()),
    OrderORM.AccountID,
)
```

Target order is preserved within each many-relation result. The loader verifies that every target key returned by the query belongs to the requested chunk. Extra rows fail with an error wrapping `query.ErrRelationUnexpectedKey` rather than being silently attached.

## Error and panic boundaries

`RelationError` includes the relation name and one-based chunk number. Its public error string does not include key values or underlying database error text. The original cause remains available through `errors.Is`, `errors.As`, and `Unwrap`.

Source-key extractors, target-key extractors, and query factories are trusted application callbacks. Their panics are contained and converted to structured relation errors without exposing panic values. The loader itself does not panic for invalid configuration or database failures.

## Cancellation and concurrency

Every chunk uses the supplied context. Cancellation stops subsequent chunks and propagates through the typed executor. Rows are closed by `FetchAll`, allowing the pool to recover or replace a connection according to pgx behavior.

A configured relation loader is immutable and safe for concurrent reuse when its key extractors and query factory are concurrency-safe. There is no shared result cache or identity map; every explicit load reflects its own database queries and transaction context.
