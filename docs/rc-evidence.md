# v1 release-candidate evidence

The final `v1.0.0` tag is blocked until the maintained application in [`examples/rcapp`](../examples/rcapp/README.md) passes from the same candidate commit on PostgreSQL 14 and PostgreSQL 18.

## Required evidence

| Gate | Evidence |
|---|---|
| Clean application build | The RC workflow checks out the candidate commit, verifies modules, builds the exact `orm` CLI, and compiles the application under `go test -race`. |
| Generated metadata | `orm generate --check` compares committed metadata with deterministic generator output. |
| Fresh database | The initial artifact is generated, validated, and applied to a recreated `public` schema. |
| Existing-schema upgrade | Seeded legacy rows are upgraded through a safe migration followed by explicit rename/review migration. |
| Data preservation | Account, order, foreign-key, and nullable-column data are asserted after upgrade, rollback, and reapply. |
| Typed runtime | Select, insert, update, delete, transaction, rollback-on-error, batch, and explicit relation loading execute against PostgreSQL. |
| SQL-injection boundary | An attack-shaped email value is inserted and queried as data; the accounts table remains present. |
| Query count | Explicit one-to-many relation loading emits exactly one observer event for one chunk. |
| Observer redaction | Application records operation, SQL shape, row count, and failure state without retaining args or raw database errors. |
| Rollback | The reversible upgrade restores `users.email`, `orders.user_id`, the legacy foreign key, and preserved data. |
| Checksum recovery | Tampered `up.sql` makes status fail; restoring reviewed bytes makes status pass. |
| History recovery | Removing an applied-prefix artifact makes status fail; restoring it makes status pass. |
| Destructive gates | Safe generation and safe apply refuse a destructive migration; destructive apply without confirmation exits with usage failure; destructive SQL is not executed. |
| PostgreSQL matrix | The same commit and workflow run on pinned PostgreSQL 14 and 18 images. |

## Release decision

A release candidate is acceptable only when:

- ordinary CI, security, fuzzing, API compatibility, documentation, PostgreSQL integration, release reproducibility, and RC application jobs are green;
- there are no unresolved review threads or open release-blocker issues;
- release notes and known limitations match the candidate commit;
- the final annotated tag resolves exactly to the reviewed green commit.

The RC application deliberately does not claim to validate production-specific traffic, backups, failover, connection limits, row-level security, custom extensions, or every PostgreSQL feature. Those remain deployment responsibilities and must not be inferred from a green framework release gate.
