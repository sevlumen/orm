# Migration failure and recovery runbook

This runbook assumes migrations are stored in version control, applied with `orm`, and backed by the checksummed transactional runner.

## Before every production migration

1. Run `orm validate` against the exact artifact directory that will be deployed.
2. Run `orm status --json` against the target database.
3. Confirm applied history is the expected prefix and review every pending `up.sql` and `down.sql`.
4. Confirm the configured maximum risk and required `--yes` gate.
5. Test the same artifact set against a recent production-like database copy.
6. Estimate lock duration and table rewrite impact for review/destructive operations.
7. Create and verify the backup or point-in-time-recovery checkpoint required by the organization's recovery objective.
8. Stop if application and database versions are not deployment-compatible.

The ORM cannot verify backup quality and cannot restore deleted data.

## Failed migration execution

Each migration and its history insert run in one PostgreSQL transaction. When a statement fails, the runner attempts rollback and does not record the migration as applied.

Actions:

1. Preserve the complete deployment logs with secrets removed.
2. Run `orm status --json` again.
3. Verify that the failed migration is still pending and no unexpected object remains.
4. Inspect PostgreSQL logs and the migration's `up.sql`.
5. Correct the cause in a new reviewed artifact when the failed artifact was already distributed. Do not rewrite an artifact that another environment may have applied.
6. Rehearse recovery on a disposable database before retrying production.

A PostgreSQL statement can have effects outside ordinary transactional DDL, including external systems invoked by functions or extensions. Treat such migrations as manual operations with their own rollback plan.

## Checksum mismatch

A checksum mismatch means a local artifact differs from the artifact represented by history or from its manifest.

Do not bypass or update the history checksum to make the command pass.

1. Stop deployment.
2. Identify the canonical artifact from the release commit or signed release archive.
3. Compare `manifest.json`, `up.sql`, `down.sql`, and `snapshot.json` byte-for-byte.
4. Restore the canonical local files when the database history is correct.
5. When the database was changed outside the canonical artifact, investigate schema and data state before any further migration.
6. Record the incident; artifact mutation after application is a process-integrity failure.

## Applied history is not a local prefix

This indicates missing, reordered, or divergent artifacts.

1. Stop all migration jobs for the database.
2. Export the history table and retain it with the deployment evidence.
3. Obtain the exact artifact sequence from every relevant release commit.
4. Determine whether history or the deployed artifact directory diverged first.
5. Restore the matching artifact sequence when history is legitimate.
6. For an incorrect database history row, use a reviewed manual repair approved by the database owner. Never delete or insert history rows merely to unblock automation.
7. Validate schema and data against the expected snapshot before resuming.

## Advisory-lock contention

`status`, `apply`, and `rollback` use one session-level advisory lock for the complete operation.

1. Confirm another legitimate migration process is running.
2. Inspect `pg_stat_activity` and deployment orchestration.
3. Let a healthy owner finish. Do not terminate it only because a timeout was reached.
4. When the owner is abandoned, verify its transaction state and database connection before terminating the backend according to operational policy.
5. Re-run `orm status` before applying anything.

Never configure multiple independent lock keys for the same history table or database migration stream.

## Cancellation or lost connection

The CLI propagates timeout, SIGINT, and SIGTERM cancellation. The runner uses an independent bounded cleanup context to roll back and release or close the pinned connection.

After cancellation:

1. Wait for PostgreSQL to finish rollback or backend termination.
2. Run `orm status` from a fresh process.
3. Check for active sessions holding the advisory lock.
4. Verify no pending migration was recorded as applied without its schema changes.
5. Retry only after state is unambiguous.

## Rollback

Every rollback requires `--yes` and executes `down.sql` from the latest applied migration backward.

Before rollback:

- inspect the exact down script;
- verify application compatibility with the older schema;
- confirm no newer application instance is still writing fields or tables that will disappear;
- back up affected data;
- understand that generated rollback restores schema shape only where the SQL is reversible.

A destructive `up.sql` may have permanently removed data. A later `down.sql` can recreate a column or table but cannot reconstruct deleted rows or values. Use backup/PITR or an application-specific data repair.

## Schema drift outside migrations

The v1 runner verifies artifacts and history, not the complete live database schema.

When drift is suspected:

1. stop migrations and schema-changing application jobs;
2. compare live catalog state with the expected snapshot and migration SQL;
3. identify whether drift is additive, conflicting, or destructive;
4. create a reviewed reconciliation migration or perform an approved manual repair;
5. test from both a clean database and the drifted state;
6. document why the out-of-band change occurred and prevent recurrence.

Do not generate a new snapshot from an unknown drifted production database and treat it as the new source of truth without review.

## Bad release

For a release containing incorrect binaries or artifacts:

1. stop rollout and mark the release unsupported;
2. retain the tag, checksums, provenance, and affected-environment list;
3. determine whether any migration was applied;
4. roll back application binaries independently from database schema when compatible;
5. use the reviewed migration rollback or forward-fix strategy appropriate to the data boundary;
6. publish a corrected patch release with new immutable artifacts—never replace files attached to an existing release tag;
7. follow the security-advisory process when confidentiality or integrity was affected.

## Evidence checklist

Retain:

- release tag and commit SHA;
- `orm version` output once release metadata is available;
- artifact checksums;
- pre- and post-deployment `status --json` output;
- PostgreSQL and Go versions;
- backup/PITR checkpoint identifier;
- migration duration and lock observations;
- redacted error logs;
- recovery decisions and approvals.
