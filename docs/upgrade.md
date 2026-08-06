# Upgrade guide

## Patch and minor releases within v1

1. Read [CHANGELOG.md](../CHANGELOG.md) and the release notes.
2. Confirm the release still supports your Go and PostgreSQL versions.
3. Update the module intentionally and review `go.mod`/`go.sum`.
4. Run code generation in check mode before regenerating:

```text
orm generate --config orm.json --check
```

5. Regenerate and review generated-code diffs when required.
6. Run the application's unit, race, integration, migration, and query-count tests.
7. Run `orm validate` against the complete migration directory.
8. Test `orm status`, `apply`, and the documented rollback scenario against a production-like database copy.
9. Review benchmark changes when upgrading code on a hot path.
10. Deploy application and schema changes in a backward-compatible order.

Do not edit an applied migration to match a newer generator. Existing artifacts are immutable deployment records.

## Generated-code changes

Generated metadata and scanner output may change without changing application entities when the generator fixes determinism, validation, or performance. Review generated diffs like handwritten code.

A safe workflow is:

```text
orm generate --config orm.json --check
orm generate --config orm.json
git diff -- generated_file.go
go test -race ./...
```

Commit source entities and generated output together.

## Snapshot and migration changes

Snapshots are canonicalized and versioned. A new release must continue reading snapshot version 1 and artifact format version 1 throughout v1.

When an entity change requires a migration:

1. export the new application snapshot;
2. run `orm diff` from the latest local artifact snapshot;
3. provide explicit rename intents where intent cannot be inferred;
4. review risk, warnings, `up.sql`, and `down.sql`;
5. commit the new artifact directory;
6. test upgrade from the previous application release.

Never replace a deployed artifact directory with regenerated files.

## CLI automation

Automation should use `--json` and parse the versioned envelope. Human output may gain detail.

Treat exit codes as:

- `0`: success;
- `1`: execution, validation, database, integrity, or cancellation failure;
- `2`: invalid usage or missing confirmation.

Do not parse credentials or SQL values from error strings. The CLI redacts known connection secrets, but operational logs still require the application's normal redaction and access controls.

## Database-first deployment compatibility

For changes that add nullable columns, tables, indexes, or compatible enum values:

1. apply the additive migration;
2. deploy application code that can use both old and new states;
3. backfill data when needed;
4. enforce stricter constraints in a later release;
5. remove obsolete fields only after all application versions stop using them.

For renames, use an expand-and-contract deployment when rolling application instances cannot switch atomically. A direct database rename can break old instances even when the migration itself is reversible.

## Destructive changes

Destructive changes require explicit risk allowance and confirmation, but those flags are not a substitute for a recovery plan.

Before applying:

- identify affected data and readers/writers;
- verify backup or PITR recovery;
- stop incompatible application versions;
- rehearse the exact migration and restore procedure;
- understand that generated `down.sql` cannot recover deleted values.

## Compatibility failures

When a newer release fails API, config, artifact, or database compatibility checks, do not update baselines or versions automatically. Determine whether the change is:

- a defect to fix;
- a backward-compatible addition requiring reviewed baseline update;
- a behavior change requiring migration guidance;
- an incompatible change that belongs in a future major version.

See [Compatibility policy](compatibility.md) and [Recovery runbook](recovery.md).
