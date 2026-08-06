# Foreign keys

Foreign keys are configured through `entity.TableBuilder`. Local columns accept either exported Go field names or mapped database column names. Referenced columns are database column names because the referenced Go type may live in another package or may not be available to the current builder.

```go
package model

import (
    "github.com/sevlumen/orm/entity"
    "github.com/sevlumen/orm/schema"
)

type Account struct {
    TenantID string `orm:"type:uuid"`
    ID       string `orm:"type:uuid"`
}

func (Account) TableName() string { return "accounts" }

func (Account) ConfigureORM(table *entity.TableBuilder) {
    table.PrimaryKey("pk_accounts", "TenantID", "ID")
}

type Order struct {
    ID        string `orm:"type:uuid;primaryKey"`
    TenantID  string `orm:"type:uuid"`
    AccountID string `orm:"column:account_id;type:uuid"`
}

func (Order) TableName() string { return "orders" }

func (Order) ConfigureORM(table *entity.TableBuilder) {
    table.ForeignKey(
        "fk_orders_account",
        "TenantID",
        "AccountID",
    ).References(
        "accounts",
        "tenant_id",
        "id",
    ).OnDelete(
        schema.Cascade,
    ).OnUpdate(
        schema.Restrict,
    ).InitiallyDeferred()
}
```

## Supported behavior

- named single-column and composite foreign keys;
- `NO ACTION`, `RESTRICT`, `CASCADE`, `SET NULL`, and `SET DEFAULT` actions;
- `DEFERRABLE` and `DEFERRABLE INITIALLY DEFERRED` constraints;
- references to primary keys, named unique constraints, inline unique columns, or non-partial unique column indexes;
- cyclic table relationships.

`SET NULL` requires nullable local columns. `SET DEFAULT` requires a nullable column or an explicit default. Referenced column order must match the backing primary or unique key.

## Creation order

Schema creation uses two phases:

1. Create every table, primary/unique/check constraint, and index.
2. Add all foreign keys with `ALTER TABLE`.

This permits cyclic relationships without disabling referential integrity or relying on table declaration order.

## Migration order

Foreign-key migrations are classified as `review` because adding a constraint validates existing rows and may lock both tables. Generated migrations:

1. drop affected foreign keys;
2. change or remove dependent tables, columns, keys, and indexes;
3. create the new dependencies;
4. add foreign keys last.

The same dependency guarantees apply in reverse for generated `down.sql`.

A foreign key is recreated automatically when its local/referenced column type changes or when the referenced primary/unique backing definition changes.

## Safety boundary

Table and column names are quoted identifiers. Foreign-key actions are closed enum values. Migration SQL and column types remain trusted developer-authored schema input and must never be built from request data.
