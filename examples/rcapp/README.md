# Sevlumen ORM release-candidate application

This package is the maintained application-level release gate for Sevlumen ORM v1. It exercises the public CLI, generated metadata, typed PostgreSQL runtime, migration upgrade and rollback, and documented recovery procedures against PostgreSQL 14 and 18.

## Covered workflow

The integration test performs the following operations using the exact `orm` binary built from the candidate commit:

1. Verify committed generated metadata with `orm generate --check`.
2. Create and validate an initial migration artifact.
3. Apply the initial schema to a fresh database.
4. Insert legacy application data.
5. Apply a safe nullable-column migration.
6. Upgrade the populated schema through explicit table, column, and constraint renames.
7. Verify that non-destructive data survives the upgrade.
8. Execute typed select, insert, update, delete, transaction, batch, and explicit relation operations.
9. Execute an SQL-injection-shaped value and verify it remains data.
10. Assert explicit relation loading uses one query and observer events do not retain argument values.
11. Roll back the reversible upgrade and verify schema shape and data.
12. Reapply the upgrade.
13. Detect checksum drift, restore reviewed bytes, and recover.
14. Detect a non-prefix local migration history, restore the artifact, and recover.
15. Verify destructive migration generation and execution require explicit risk and confirmation gates without applying the destructive change.

## Local execution

A PostgreSQL database dedicated to the test is required. The test drops and recreates the `public` schema.

```bash
export SEVLUMEN_TEST_DATABASE_URL='postgres://postgres:postgres@127.0.0.1:5432/sevlumen_test?sslmode=disable'
go build -trimpath -o /tmp/sevlumen-orm ./cmd/orm
export SEVLUMEN_RC_ORM_BINARY=/tmp/sevlumen-orm
go test -race ./examples/rcapp -run '^TestReleaseCandidateWorkflow$' -count=1 -v
```

Verify generated metadata independently:

```bash
/tmp/sevlumen-orm generate \
  --dir ./examples/rcapp \
  --output orm_gen.go \
  --type Account \
  --type Order \
  --check
```

## Boundaries

- The test database must not contain production data.
- Destructive SQL is generated and gated but intentionally not applied.
- The example validates one representative application workflow; it does not replace application-specific load, failover, backup, or compatibility testing.
- Raw SQL and migration expressions remain trusted developer-authored inputs. Runtime values in typed query and mutation APIs are parameterized.
