// Package runner applies checksummed Sevlumen migration artifacts to PostgreSQL.
package runner

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	"github.com/sevlumen/orm/migration"
	"github.com/sevlumen/orm/migration/artifact"
	postgres "github.com/sevlumen/postgres"
)

const (
	defaultHistorySchema = "public"
	defaultHistoryTable  = "__sevlumen_migrations"
)

type Config struct {
	MigrationsDir string
	HistorySchema string
	HistoryTable  string
	LockKey       int64
	MaximumRisk   migration.Risk
}

type Runner struct {
	db            *sql.DB
	migrationsDir string
	historySQL    string
	lockKey       int64
	now           func() time.Time
	maximumRisk   migration.Risk
}

type MigrationState string

const (
	StatePending MigrationState = "pending"
	StateApplied MigrationState = "applied"
)

type Status struct {
	ID            string
	Checksum      string
	Risk          string
	State         MigrationState
	AppliedAt     *time.Time
	ExecutionTime time.Duration
}

type Result struct {
	ID            string
	Checksum      string
	ExecutionTime time.Duration
}

type MigrationError struct {
	ID        string
	Direction string
	Stage     string
	Err       error
}

func (e *MigrationError) Error() string {
	return fmt.Sprintf("runner: migration %s %s failed during %s: %v", e.ID, e.Direction, e.Stage, e.Err)
}
func (e *MigrationError) Unwrap() error { return e.Err }

func New(db *sql.DB, config Config) (*Runner, error) {
	if db == nil {
		return nil, fmt.Errorf("runner: PostgreSQL database is required")
	}
	if strings.TrimSpace(config.MigrationsDir) == "" {
		return nil, fmt.Errorf("runner: migrations directory is required")
	}
	if config.HistorySchema == "" {
		config.HistorySchema = defaultHistorySchema
	}
	if config.HistoryTable == "" {
		config.HistoryTable = defaultHistoryTable
	}
	if config.MaximumRisk > migration.RiskDestructive {
		return nil, fmt.Errorf("runner: invalid maximum migration risk %d", config.MaximumRisk)
	}
	if err := validateIdentifier(config.HistorySchema); err != nil {
		return nil, fmt.Errorf("runner: invalid history schema: %w", err)
	}
	if err := validateIdentifier(config.HistoryTable); err != nil {
		return nil, fmt.Errorf("runner: invalid history table: %w", err)
	}
	lockKey := config.LockKey
	if lockKey == 0 {
		lockKey = deriveLockKey(config.HistorySchema + "." + config.HistoryTable)
	}
	return &Runner{db: db, migrationsDir: config.MigrationsDir, historySQL: quoteIdentifier(config.HistorySchema) + "." + quoteIdentifier(config.HistoryTable), lockKey: lockKey, now: time.Now, maximumRisk: config.MaximumRisk}, nil
}

func (r *Runner) Status(ctx context.Context) ([]Status, error) {
	session, release, err := r.lockedSession(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	return r.statusLocked(ctx, session)
}

func (r *Runner) Apply(ctx context.Context) ([]Result, error) {
	session, release, err := r.lockedSession(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	statuses, err := r.statusLocked(ctx, session)
	if err != nil {
		return nil, err
	}
	for _, status := range statuses {
		if status.State != StatePending {
			continue
		}
		risk, err := parseRisk(status.Risk)
		if err != nil {
			return nil, err
		}
		if risk > r.maximumRisk {
			return nil, fmt.Errorf("runner: pending migration %q has %s risk, exceeding configured maximum %s", status.ID, risk, r.maximumRisk)
		}
	}
	results := make([]Result, 0)
	for _, status := range statuses {
		if status.State == StateApplied {
			continue
		}
		value, err := artifact.Load(r.migrationsDir, status.ID)
		if err != nil {
			return results, &MigrationError{ID: status.ID, Direction: "up", Stage: "load", Err: err}
		}
		checksum, err := value.Checksum()
		if err != nil {
			return results, &MigrationError{ID: status.ID, Direction: "up", Stage: "checksum", Err: err}
		}
		if checksum != status.Checksum {
			return results, &MigrationError{ID: status.ID, Direction: "up", Stage: "preflight", Err: fmt.Errorf("artifact changed after preflight: before=%s after=%s", status.Checksum, checksum)}
		}
		result, err := r.applyOne(ctx, session, value)
		if err != nil {
			return results, err
		}
		results = append(results, result)
	}
	return results, nil
}

func (r *Runner) Rollback(ctx context.Context, count int) ([]Result, error) {
	if count <= 0 {
		return nil, fmt.Errorf("runner: rollback count must be positive")
	}
	session, release, err := r.lockedSession(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	applied, err := r.readHistory(ctx, session)
	if err != nil {
		return nil, err
	}
	if count > len(applied) {
		return nil, fmt.Errorf("runner: cannot roll back %d migrations; only %d are applied", count, len(applied))
	}
	if _, err := r.statusLocked(ctx, session); err != nil {
		return nil, err
	}
	results := make([]Result, 0, count)
	for i := len(applied) - 1; i >= len(applied)-count; i-- {
		history := applied[i]
		value, err := artifact.Load(r.migrationsDir, history.ID)
		if err != nil {
			return results, &MigrationError{ID: history.ID, Direction: "down", Stage: "load", Err: err}
		}
		result, err := r.rollbackOne(ctx, session, value, history.Checksum)
		if err != nil {
			return results, err
		}
		results = append(results, result)
	}
	return results, nil
}

func (r *Runner) lockedSession(ctx context.Context) (*postgres.Session, func(), error) {
	session, err := postgres.Acquire(ctx, r.db)
	if err != nil {
		return nil, nil, fmt.Errorf("runner: acquire PostgreSQL session: %w", err)
	}
	locked := false
	release := func() {
		if !locked {
			_ = session.Close()
			return
		}
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var unlocked bool
		err := session.QueryRowContext(unlockCtx, "SELECT pg_advisory_unlock($1)", r.lockKey).Scan(&unlocked)
		if err != nil || !unlocked {
			_ = session.Discard()
			return
		}
		_ = session.Close()
	}
	if _, err := session.ExecContext(ctx, "SELECT pg_advisory_lock($1)", r.lockKey); err != nil {
		release()
		return nil, nil, fmt.Errorf("runner: acquire advisory lock: %w", err)
	}
	locked = true
	if err := r.ensureHistory(ctx, session); err != nil {
		release()
		return nil, nil, err
	}
	return session, release, nil
}

func (r *Runner) ensureHistory(ctx context.Context, session *postgres.Session) error {
	query := `CREATE TABLE IF NOT EXISTS ` + r.historySQL + ` (
    migration_id text PRIMARY KEY,
    checksum text NOT NULL CHECK (length(checksum) = 64),
    applied_at timestamptz NOT NULL,
    execution_time_ms bigint NOT NULL CHECK (execution_time_ms >= 0)
)`
	if _, err := session.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("runner: ensure migration history table: %w", err)
	}
	return nil
}

func (r *Runner) statusLocked(ctx context.Context, session *postgres.Session) ([]Status, error) {
	ids, err := artifact.List(r.migrationsDir)
	if err != nil {
		return nil, fmt.Errorf("runner: list migrations: %w", err)
	}
	local := make([]loadedArtifact, 0, len(ids))
	for _, id := range ids {
		value, err := artifact.Load(r.migrationsDir, id)
		if err != nil {
			return nil, &MigrationError{ID: id, Direction: "status", Stage: "load", Err: err}
		}
		checksum, err := value.Checksum()
		if err != nil {
			return nil, &MigrationError{ID: id, Direction: "status", Stage: "checksum", Err: err}
		}
		local = append(local, loadedArtifact{Artifact: value, Checksum: checksum})
	}
	history, err := r.readHistory(ctx, session)
	if err != nil {
		return nil, err
	}
	if len(history) > len(local) {
		return nil, fmt.Errorf("runner: database has %d applied migrations but only %d local artifacts", len(history), len(local))
	}
	for i, applied := range history {
		if local[i].Manifest.ID != applied.ID {
			return nil, fmt.Errorf("runner: migration history is not a local prefix at position %d: database=%q local=%q", i, applied.ID, local[i].Manifest.ID)
		}
		if local[i].Checksum != applied.Checksum {
			return nil, fmt.Errorf("runner: checksum mismatch for applied migration %q: database=%s local=%s", applied.ID, applied.Checksum, local[i].Checksum)
		}
	}
	result := make([]Status, 0, len(local))
	for i, value := range local {
		status := Status{ID: value.Manifest.ID, Checksum: value.Checksum, Risk: value.Manifest.Risk, State: StatePending}
		if i < len(history) {
			appliedAt := history[i].AppliedAt
			status.State = StateApplied
			status.AppliedAt = &appliedAt
			status.ExecutionTime = history[i].ExecutionTime
		}
		result = append(result, status)
	}
	return result, nil
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func (r *Runner) readHistory(ctx context.Context, db queryer) ([]historyRow, error) {
	rows, err := db.QueryContext(ctx, `SELECT migration_id, checksum, applied_at, execution_time_ms FROM `+r.historySQL+` ORDER BY migration_id`)
	if err != nil {
		return nil, fmt.Errorf("runner: query migration history: %w", err)
	}
	defer rows.Close()
	var result []historyRow
	for rows.Next() {
		var row historyRow
		var milliseconds int64
		if err := rows.Scan(&row.ID, &row.Checksum, &row.AppliedAt, &milliseconds); err != nil {
			return nil, fmt.Errorf("runner: scan migration history: %w", err)
		}
		row.ExecutionTime = time.Duration(milliseconds) * time.Millisecond
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("runner: read migration history: %w", err)
	}
	return result, nil
}

func (r *Runner) applyOne(ctx context.Context, session *postgres.Session, value artifact.Artifact) (Result, error) {
	id := value.Manifest.ID
	if err := validateMigrationScript(string(value.UpSQL)); err != nil {
		return Result{}, &MigrationError{ID: id, Direction: "up", Stage: "validate", Err: err}
	}
	checksum, err := value.Checksum()
	if err != nil {
		return Result{}, &MigrationError{ID: id, Direction: "up", Stage: "checksum", Err: err}
	}
	if _, err := session.ExecContext(ctx, `BEGIN`); err != nil {
		return Result{}, &MigrationError{ID: id, Direction: "up", Stage: "begin", Err: err}
	}
	committed := false
	defer func() {
		if !committed {
			rollbackSession(session)
		}
	}()
	started := r.now()
	if _, err := session.ExecScriptContext(ctx, string(value.UpSQL)); err != nil {
		return Result{}, &MigrationError{ID: id, Direction: "up", Stage: "execute", Err: err}
	}
	duration := nonNegativeDuration(r.now().Sub(started))
	if _, err := session.ExecContext(ctx, `INSERT INTO `+r.historySQL+` (migration_id, checksum, applied_at, execution_time_ms) VALUES ($1, $2, clock_timestamp(), $3)`, id, checksum, duration.Milliseconds()); err != nil {
		return Result{}, &MigrationError{ID: id, Direction: "up", Stage: "history", Err: err}
	}
	if _, err := session.ExecContext(ctx, `COMMIT`); err != nil {
		if errors.Is(err, postgres.ErrOperationOutcomeUnknown) {
			_ = session.Discard()
			if ok, reconcileErr := r.appliedWithChecksum(context.Background(), id, checksum); reconcileErr == nil && ok {
				committed = true
				return Result{ID: id, Checksum: checksum, ExecutionTime: duration}, nil
			}
		}
		return Result{}, &MigrationError{ID: id, Direction: "up", Stage: "commit", Err: err}
	}
	committed = true
	return Result{ID: id, Checksum: checksum, ExecutionTime: duration}, nil
}

func (r *Runner) rollbackOne(ctx context.Context, session *postgres.Session, value artifact.Artifact, expectedChecksum string) (Result, error) {
	id := value.Manifest.ID
	if err := validateMigrationScript(string(value.DownSQL)); err != nil {
		return Result{}, &MigrationError{ID: id, Direction: "down", Stage: "validate", Err: err}
	}
	checksum, err := value.Checksum()
	if err != nil {
		return Result{}, &MigrationError{ID: id, Direction: "down", Stage: "checksum", Err: err}
	}
	if checksum != expectedChecksum {
		return Result{}, &MigrationError{ID: id, Direction: "down", Stage: "checksum", Err: fmt.Errorf("database=%s local=%s", expectedChecksum, checksum)}
	}
	if _, err := session.ExecContext(ctx, `BEGIN`); err != nil {
		return Result{}, &MigrationError{ID: id, Direction: "down", Stage: "begin", Err: err}
	}
	committed := false
	defer func() {
		if !committed {
			rollbackSession(session)
		}
	}()
	started := r.now()
	if _, err := session.ExecScriptContext(ctx, string(value.DownSQL)); err != nil {
		return Result{}, &MigrationError{ID: id, Direction: "down", Stage: "execute", Err: err}
	}
	duration := nonNegativeDuration(r.now().Sub(started))
	result, err := session.ExecContext(ctx, `DELETE FROM `+r.historySQL+` WHERE migration_id = $1 AND checksum = $2`, id, checksum)
	if err != nil {
		return Result{}, &MigrationError{ID: id, Direction: "down", Stage: "history", Err: err}
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Result{}, &MigrationError{ID: id, Direction: "down", Stage: "history", Err: err}
	}
	if affected != 1 {
		return Result{}, &MigrationError{ID: id, Direction: "down", Stage: "history", Err: errors.New("history row changed concurrently")}
	}
	if _, err := session.ExecContext(ctx, `COMMIT`); err != nil {
		if errors.Is(err, postgres.ErrOperationOutcomeUnknown) {
			_ = session.Discard()
			if ok, reconcileErr := r.appliedWithChecksum(context.Background(), id, checksum); reconcileErr == nil && !ok {
				committed = true
				return Result{ID: id, Checksum: checksum, ExecutionTime: duration}, nil
			}
		}
		return Result{}, &MigrationError{ID: id, Direction: "down", Stage: "commit", Err: err}
	}
	committed = true
	return Result{ID: id, Checksum: checksum, ExecutionTime: duration}, nil
}

func (r *Runner) appliedWithChecksum(ctx context.Context, id, checksum string) (bool, error) {
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var actual string
	err := r.db.QueryRowContext(checkCtx, `SELECT checksum FROM `+r.historySQL+` WHERE migration_id = $1`, id).Scan(&actual)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if actual != checksum {
		return false, fmt.Errorf("runner: reconciled checksum mismatch for %q: database=%s local=%s", id, actual, checksum)
	}
	return true, nil
}

func rollbackSession(session *postgres.Session) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := session.ExecContext(ctx, `ROLLBACK`); err != nil {
		_ = session.Discard()
	}
}

func parseRisk(value string) (migration.Risk, error) {
	switch value {
	case migration.RiskSafe.String():
		return migration.RiskSafe, nil
	case migration.RiskReview.String():
		return migration.RiskReview, nil
	case migration.RiskDestructive.String():
		return migration.RiskDestructive, nil
	default:
		return 0, fmt.Errorf("runner: invalid artifact risk %q", value)
	}
}
func validateIdentifier(value string) error {
	if value == "" || len(value) > 63 {
		return fmt.Errorf("identifier must contain 1 to 63 bytes")
	}
	for i, char := range value {
		if char == '_' || char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || i > 0 && char >= '0' && char <= '9' {
			continue
		}
		return fmt.Errorf("identifier %q contains unsupported character %q", value, char)
	}
	return nil
}
func quoteIdentifier(value string) string { return `"` + strings.ReplaceAll(value, `"`, `""`) + `"` }
func deriveLockKey(value string) int64 {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte("sevlumen.orm.migrations:" + value))
	return int64(hash.Sum64())
}
func nonNegativeDuration(value time.Duration) time.Duration {
	if value < 0 {
		return 0
	}
	return value
}

type loadedArtifact struct {
	artifact.Artifact
	Checksum string
}
type historyRow struct {
	ID            string
	Checksum      string
	AppliedAt     time.Time
	ExecutionTime time.Duration
}
