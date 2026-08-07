package runner

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sevlumen/orm/migration"
	"github.com/sevlumen/orm/migration/artifact"
	ormpostgres "github.com/sevlumen/orm/postgres"
	"github.com/sevlumen/orm/schema"
	sevlumenpostgres "github.com/sevlumen/postgres"
)

func TestNewValidatesConfiguration(t *testing.T) {
	t.Parallel()
	if _, err := New(nil, Config{MigrationsDir: "migrations"}); err == nil {
		t.Fatal("expected nil database error")
	}
}

func TestValidateIdentifier(t *testing.T) {
	t.Parallel()
	valid := []string{"public", "__sevlumen_migrations", "history_2"}
	for _, value := range valid {
		if err := validateIdentifier(value); err != nil {
			t.Fatalf("validateIdentifier(%q) = %v", value, err)
		}
	}
	invalid := []string{"", "2history", "history-table", strings.Repeat("a", 64)}
	for _, value := range invalid {
		if err := validateIdentifier(value); err == nil {
			t.Fatalf("validateIdentifier(%q) unexpectedly succeeded", value)
		}
	}
}

func TestArtifactChecksumChangesWithManifestMetadata(t *testing.T) {
	t.Parallel()
	value := buildArtifact(t, t.TempDir(), "20260805210000_create_users", "CREATE TABLE users(id int);", "DROP TABLE users;", migration.RiskSafe)
	first, err := value.Checksum()
	if err != nil {
		t.Fatal(err)
	}
	value.Manifest.Warnings = []string{"manual review"}
	second, err := value.Checksum()
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("artifact checksum did not include manifest metadata")
	}
}

func TestRunnerIntegrationApplyStatusRollback(t *testing.T) {
	database := integrationDatabase(t)
	root := t.TempDir()
	suffix := integrationSuffix()
	table := "sevlumen_users_" + suffix
	history := "sevlumen_history_" + suffix

	buildArtifact(t, root, "20260805210000_create_users", fmt.Sprintf("CREATE TABLE %s (id bigint PRIMARY KEY);", table), fmt.Sprintf("DROP TABLE %s;", table), migration.RiskSafe)
	buildArtifact(t, root, "20260805210100_add_email", fmt.Sprintf("ALTER TABLE %s ADD COLUMN email text;", table), fmt.Sprintf("ALTER TABLE %s DROP COLUMN email;", table), migration.RiskSafe)

	runner, err := New(database, Config{MigrationsDir: root, HistoryTable: history})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	defer cleanupIntegrationObjects(t, database, table, history)

	status, err := runner.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(status) != 2 || status[0].State != StatePending || status[1].State != StatePending {
		t.Fatalf("unexpected initial status: %#v", status)
	}
	applied, err := runner.Apply(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 2 {
		t.Fatalf("applied = %d, want 2", len(applied))
	}
	if again, err := runner.Apply(ctx); err != nil || len(again) != 0 {
		t.Fatalf("second Apply() = %#v, %v", again, err)
	}
	if !columnExists(t, ctx, database, table, "email") {
		t.Fatal("email column was not created")
	}

	rolledBack, err := runner.Rollback(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(rolledBack) != 1 || rolledBack[0].ID != "20260805210100_add_email" {
		t.Fatalf("unexpected rollback: %#v", rolledBack)
	}
	if columnExists(t, ctx, database, table, "email") {
		t.Fatal("email column still exists after rollback")
	}
	if _, err := runner.Rollback(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if tableExists(t, ctx, database, table) {
		t.Fatal("table still exists after full rollback")
	}
}

func TestRunnerIntegrationFailureRollsBackMigrationAndHistory(t *testing.T) {
	database := integrationDatabase(t)
	root := t.TempDir()
	suffix := integrationSuffix()
	table := "sevlumen_failed_" + suffix
	history := "sevlumen_history_" + suffix
	buildArtifact(t, root, "20260805220000_broken", fmt.Sprintf("CREATE TABLE %s (id bigint); SELECT missing_column FROM %s;", table, table), fmt.Sprintf("DROP TABLE %s;", table), migration.RiskSafe)

	runner, err := New(database, Config{MigrationsDir: root, HistoryTable: history})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	defer cleanupIntegrationObjects(t, database, table, history)

	if _, err := runner.Apply(ctx); err == nil {
		t.Fatal("expected migration failure")
	}
	if tableExists(t, ctx, database, table) {
		t.Fatal("failed migration left its table behind")
	}
	var count int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM `+quoteTestIdentifier("public", history)).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("history count = %d, want 0", count)
	}
}

func TestRunnerIntegrationAdvisoryLockSerializesConcurrentApply(t *testing.T) {
	database := integrationDatabase(t)
	root := t.TempDir()
	suffix := integrationSuffix()
	table := "sevlumen_concurrent_" + suffix
	history := "sevlumen_history_" + suffix
	buildArtifact(t, root, "20260805230000_create_concurrent", fmt.Sprintf("CREATE TABLE %s (id bigint); SELECT pg_sleep(0.2);", table), fmt.Sprintf("DROP TABLE %s;", table), migration.RiskSafe)

	first, err := New(database, Config{MigrationsDir: root, HistoryTable: history})
	if err != nil {
		t.Fatal(err)
	}
	second, err := New(database, Config{MigrationsDir: root, HistoryTable: history})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	defer cleanupIntegrationObjects(t, database, table, history)

	var wait sync.WaitGroup
	wait.Add(2)
	results := make(chan []Result, 2)
	errorsCh := make(chan error, 2)
	for _, candidate := range []*Runner{first, second} {
		go func(value *Runner) {
			defer wait.Done()
			result, err := value.Apply(ctx)
			results <- result
			errorsCh <- err
		}(candidate)
	}
	wait.Wait()
	close(results)
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	total := 0
	for result := range results {
		total += len(result)
	}
	if total != 1 {
		t.Fatalf("total applied migrations = %d, want 1", total)
	}
}

func TestRunnerIntegrationRiskGatePreflightsAllPendingMigrations(t *testing.T) {
	database := integrationDatabase(t)
	root := t.TempDir()
	suffix := integrationSuffix()
	table := "sevlumen_risk_" + suffix
	history := "sevlumen_history_" + suffix
	buildArtifact(t, root, "20260805233000_safe", fmt.Sprintf("CREATE TABLE %s (id bigint);", table), fmt.Sprintf("DROP TABLE %s;", table), migration.RiskSafe)
	buildArtifact(t, root, "20260805233100_destructive", fmt.Sprintf("ALTER TABLE %s DROP COLUMN id;", table), fmt.Sprintf("ALTER TABLE %s ADD COLUMN id bigint;", table), migration.RiskDestructive)

	runner, err := New(database, Config{MigrationsDir: root, HistoryTable: history})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	defer cleanupIntegrationObjects(t, database, table, history)

	if _, err := runner.Apply(ctx); err == nil || !strings.Contains(err.Error(), "exceeding configured maximum") {
		t.Fatalf("expected risk-gate error, got %v", err)
	}
	if tableExists(t, ctx, database, table) {
		t.Fatal("safe migration was applied before risk preflight failed")
	}
}

func TestMigrationErrorSupportsErrorsAs(t *testing.T) {
	t.Parallel()
	original := errors.New("boom")
	err := &MigrationError{ID: "20260805210000_create_users", Direction: "up", Stage: "execute", Err: original}
	if !errors.Is(err, original) {
		t.Fatal("MigrationError does not unwrap")
	}
	var target *MigrationError
	if !errors.As(err, &target) || target.ID == "" {
		t.Fatal("errors.As did not resolve MigrationError")
	}
}

func integrationDatabase(t *testing.T) *sql.DB {
	t.Helper()
	connectionString := os.Getenv("SEVLUMEN_TEST_DATABASE_URL")
	if connectionString == "" {
		t.Skip("SEVLUMEN_TEST_DATABASE_URL is not set")
	}
	database, err := sevlumenpostgres.Open(connectionString)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := database.PingContext(ctx); err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func buildArtifact(t *testing.T, root, id, up, down string, risk migration.Risk) artifact.Artifact {
	t.Helper()
	snapshot, err := migration.NewSnapshot(schema.Schema{})
	if err != nil {
		t.Fatal(err)
	}
	value, err := artifact.Build(id, ormpostgres.MigrationSQL{Up: up, Down: down, Risk: risk}, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := artifact.Write(root, value); err != nil {
		t.Fatal(err)
	}
	return value
}

func integrationSuffix() string {
	return fmt.Sprintf("%x", time.Now().UnixNano())
}

func tableExists(t *testing.T, ctx context.Context, database *sql.DB, table string) bool {
	t.Helper()
	var exists bool
	if err := database.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, "public."+table).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	return exists
}

func columnExists(t *testing.T, ctx context.Context, database *sql.DB, table, column string) bool {
	t.Helper()
	var exists bool
	if err := database.QueryRowContext(ctx, `SELECT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2
    )`, table, column).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	return exists
}

func cleanupIntegrationObjects(t *testing.T, database *sql.DB, table, history string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, name := range []string{table, history} {
		query := "DROP TABLE IF EXISTS " + quoteTestIdentifier("public", name)
		if _, err := database.ExecContext(ctx, query); err != nil && !errors.Is(err, context.Canceled) {
			t.Logf("cleanup %s failed: %v", name, err)
		}
	}
}

func quoteTestIdentifier(parts ...string) string {
	quoted := make([]string, len(parts))
	for index, part := range parts {
		quoted[index] = `"` + strings.ReplaceAll(part, `"`, `""`) + `"`
	}
	return strings.Join(quoted, ".")
}

func TestGeneratedArtifactRootIsPortable(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	value := buildArtifact(t, root, "20260805210000_create_users", "SELECT 1;", "SELECT 1;", migration.RiskSafe)
	if _, err := os.Stat(filepath.Join(root, value.Manifest.ID, artifact.ManifestFile)); err != nil {
		t.Fatal(err)
	}
}
