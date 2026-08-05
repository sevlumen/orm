# Entity schema configuration

Simple single-column metadata can remain in `orm` struct tags. Use `entity.Configurer` when a table needs named or composite constraints and PostgreSQL indexes.

```go
package model

import "github.com/sevlumen/orm/entity"

type Membership struct {
    TenantID string  `orm:"type:uuid"`
    UserID   string  `orm:"column:user_id;type:uuid"`
    Email    string  `orm:"type:varchar(320)"`
    Deleted  *string `orm:"column:deleted_at;type:timestamptz"`
}

func (Membership) TableName() string { return "memberships" }

func (Membership) ConfigureORM(table *entity.TableBuilder) {
    table.PrimaryKey("pk_memberships", "TenantID", "UserID")
    table.Unique("uq_memberships_tenant_email", "TenantID", "Email")
    table.Check("ck_memberships_email", "length(email) > 3")

    table.Index("ix_memberships_active_email", "Email").
        Include("UserID").
        Where("deleted_at IS NULL")

    table.ExpressionIndex(
        "ix_memberships_lower_email",
        "lower(email)",
    ).Unique().Using("btree")
}
```

Field arguments accept either exported Go field names (`TenantID`) or mapped database column names (`tenant_id`). Unknown fields, duplicate declarations, invalid identifiers, and conflicting relation names are returned as deterministic parse errors.

## Supported table metadata

- named single or composite primary keys;
- named single or composite unique constraints;
- named check constraints;
- single or composite column indexes;
- unique indexes;
- partial indexes through `Where`;
- expression indexes;
- covering indexes through `Include`;
- PostgreSQL access methods through `Using`.

## Migration behavior

Creating, dropping, or replacing an index or named constraint is classified as `review`. These operations can scan existing rows, acquire strong locks, fail on existing data, or materially change query performance.

Primary-key changes are not inferred. Column and table renames are also not inferred. Use explicit SQL migrations when the desired operation cannot be distinguished safely from drop-and-create behavior.

## Trusted SQL boundary

Check expressions, expression-index definitions, partial-index predicates, column types, defaults, and migration SQL are trusted developer-authored inputs. Never build them from HTTP request values, tenant data, or other untrusted input.

## PostgreSQL name boundaries

Table, column, constraint, and index names are limited to 63 bytes to prevent PostgreSQL identifier truncation from producing unexpected collisions. Index method names accept identifier characters only; they are not arbitrary SQL fragments.
