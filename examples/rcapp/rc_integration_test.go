package rcapp

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sevlumen/orm/migration"
	"github.com/sevlumen/orm/postgres/query"
	sevlumenpostgres "github.com/sevlumen/postgres"
)

const (
	initialMigrationID     = "20260806010000_initial_schema"
	safeMigrationID        = "20260806020000_add_legacy_note"
	upgradeMigrationID     = "20260806030000_upgrade_accounts"
	destructiveMigrationID = "20260806040000_drop_legacy_note"
	rcHistoryTable         = "__sevlumen_rc_migrations"
	rcLockKey       int64  = 9120260807
)

func TestReleaseCandidateWorkflow(t *testing.T) {
	databaseURL := os.Getenv("SEVLUMEN_TEST_DATABASE_URL")
	ormBinary := os.Getenv("SEVLUMEN_RC_ORM_BINARY")
	if databaseURL == "" || ormBinary == "" {
		t.Skip("set SEVLUMEN_TEST_DATABASE_URL and SEVLUMEN_RC_ORM_BINARY to run the RC application")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	database, err := sevlumenpostgres.Open(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	resetDatabase(t, ctx, database)
	defer resetDatabase(t, context.Background(), database)

	runCLI(t, ctx, ormBinary, 0,
		"generate", "--dir", packageDirectory(t), "--output", "orm_gen.go",
		"--type", "Account", "--type", "Order", "--check",
	)

	workspace := t.TempDir()
	migrationsDirectory := filepath.Join(workspace, "migrations")
	initialPath := writeLoadedSnapshot(t, workspace, "initial.snapshot.json", InitialSnapshot)
	safePath := writeLoadedSnapshot(t, workspace, "safe.snapshot.json", SafeSnapshot)
	finalPath := writeLoadedSnapshot(t, workspace, "final.snapshot.json", FinalSnapshot)
	destructivePath := writeLoadedSnapshot(t, workspace, "destructive.snapshot.json", DestructiveSnapshot)
	renamesPath := writeRenames(t, workspace, UpgradeRenames())

	// Build and apply a fresh legacy database.
	runCLI(t, ctx, ormBinary, 0,
		"diff", "--after", initialPath, "--id", initialMigrationID,
		"--migrations", migrationsDirectory, "--max-risk", "review",
	)
	runCLI(t, ctx, ormBinary, 0, "validate", "--migrations", migrationsDirectory)
	runDatabaseCLI(t, ctx, ormBinary, databaseURL, migrationsDirectory, 0,
		"apply", "--max-risk", "review",
	)
	mustExec(t, ctx, database, `INSERT INTO users (id, email, active) VALUES (1, $1, true)`, "legacy@example.test")
	mustExec(t, ctx, database, `INSERT INTO orders (id, user_id, total) VALUES (1, 1, 1200)`)

	// Apply a safe migration to populated data.
	runCLI(t, ctx, ormBinary, 0,
		"diff", "--after", safePath, "--id", safeMigrationID,
		"--migrations", migrationsDirectory, "--max-risk", "safe",
	)
	runDatabaseCLI(t, ctx, ormBinary, databaseURL, migrationsDirectory, 0,
		"apply", "--max-risk", "safe",
	)
	mustExec(t, ctx, database, `UPDATE users SET legacy_note = $1 WHERE id = 1`, "preserve-me")

	// Generate an explicit rename/review upgrade and prove the risk gate fails closed.
	runCLI(t, ctx, ormBinary, 0,
		"diff", "--after", finalPath, "--id", upgradeMigrationID,
		"--renames", renamesPath, "--migrations", migrationsDirectory,
		"--max-risk", "review",
	)
	runDatabaseCLI(t, ctx, ormBinary, databaseURL, migrationsDirectory, 1,
		"apply", "--max-risk", "safe",
	)
	assertRelationExists(t, ctx, database, "users", true)
	assertRelationExists(t, ctx, database, "accounts", false)

	runDatabaseCLI(t, ctx, ormBinary, databaseURL, migrationsDirectory, 0,
		"apply", "--max-risk", "review",
	)
	runDatabaseCLI(t, ctx, ormBinary, databaseURL, migrationsDirectory, 0, "status")
	assertRelationExists(t, ctx, database, "users", false)
	assertRelationExists(t, ctx, database, "accounts", true)
	assertColumnExists(t, ctx, database, "accounts", "login_email", true)
	assertColumnExists(t, ctx, database, "orders", "account_id", true)
	assertLegacyData(t, ctx, database)

	exerciseTypedRuntime(t, ctx, database)

	// Rollback restores the legacy shape and non-destructive data, then reapply.
	runDatabaseCLI(t, ctx, ormBinary, databaseURL, migrationsDirectory, 0,
		"rollback", "--steps", "1", "--yes",
	)
	assertRelationExists(t, ctx, database, "accounts", false)
	assertRelationExists(t, ctx, database, "users", true)
	assertColumnExists(t, ctx, database, "users", "email", true)
	assertColumnExists(t, ctx, database, "users", "legacy_note", true)
	assertColumnExists(t, ctx, database, "orders", "user_id", true)
	var preserved string
	if err := database.QueryRowContext(ctx, `SELECT legacy_note FROM users WHERE id = 1`).Scan(&preserved); err != nil {
		t.Fatal(err)
	}
	if preserved != "preserve-me" {
		t.Fatalf("legacy note after rollback=%q", preserved)
	}
	runDatabaseCLI(t, ctx, ormBinary, databaseURL, migrationsDirectory, 0,
		"apply", "--max-risk", "review",
	)
	assertLegacyData(t, ctx, database)

	verifyChecksumDriftRecovery(t, ctx, ormBinary, databaseURL, migrationsDirectory)
	verifyHistoryRecovery(t, ctx, ormBinary, databaseURL, migrationsDirectory)
	verifyDestructiveGate(t, ctx, database, ormBinary, databaseURL, migrationsDirectory, destructivePath)
}

func exerciseTypedRuntime(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	recorder := &Recorder{}
	executor, err := NewExecutor(database, recorder)
	if err != nil {
		t.Fatal(err)
	}

	legacy, found, err := FindAccountByEmail(ctx, executor, "legacy@example.test")
	if err != nil || !found {
		t.Fatalf("legacy account found=%t err=%v", found, err)
	}
	if legacy.ID != 1 || legacy.LegacyNote == nil || *legacy.LegacyNote != "preserve-me" {
		t.Fatalf("legacy account=%#v", legacy)
	}

	payload := `attack' OR 1=1; DROP TABLE accounts; --`
	displayName := "Payload User"
	created, err := CreateAccount(ctx, executor, Account{
		ID:          2,
		LoginEmail:  payload,
		DisplayName: &displayName,
		Active:      true,
	})
	if err != nil {
		t.Fatal(err)
	}
	matched, found, err := FindAccountByEmail(ctx, executor, payload)
	if err != nil || !found || matched.ID != created.ID {
		t.Fatalf("payload lookup account=%#v found=%t err=%v", matched, found, err)
	}
	assertRelationExists(t, ctx, database, "accounts", true)

	if err := CreateAccountAndOrder(ctx, database, recorder,
		Account{ID: 3, LoginEmail: "transaction@example.test", Active: true},
		Order{ID: 3, AccountID: 3, Total: 3400, Status: "paid"},
	); err != nil {
		t.Fatal(err)
	}
	if err := CreateAccountAndOrder(ctx, database, recorder,
		Account{ID: 4, LoginEmail: "rollback@example.test", Active: true},
		Order{ID: 4, AccountID: 4, Total: -1, Status: "invalid"},
	); err == nil {
		t.Fatal("expected transaction check-constraint failure")
	}
	if _, found, err := FindAccountByEmail(ctx, executor, "rollback@example.test"); err != nil || found {
		t.Fatalf("rolled-back account found=%t err=%v", found, err)
	}

	if err := InsertOrdersBatch(ctx, executor, []Order{
		{ID: 20, AccountID: 2, Total: 500, Status: "pending"},
		{ID: 21, AccountID: 2, Total: 700, Status: "paid"},
	}); err != nil {
		t.Fatal(err)
	}
	updatedName := "Legacy Renamed"
	updated, err := SetDisplayName(ctx, executor, 1, &updatedName)
	if err != nil || updated.DisplayName == nil || *updated.DisplayName != updatedName {
		t.Fatalf("updated account=%#v err=%v", updated, err)
	}

	recorder.Reset()
	relations, err := AccountOrders().Load(ctx, executor, []Account{legacy, created, {ID: 3}})
	if err != nil {
		t.Fatal(err)
	}
	if len(relations) != 3 || len(relations[0].Values) != 1 || len(relations[1].Values) != 2 || len(relations[2].Values) != 1 {
		t.Fatalf("relation results=%#v", relations)
	}
	events := recorder.Events()
	if len(events) != 1 {
		t.Fatalf("relation query count=%d events=%#v", len(events), events)
	}
	if strings.Contains(events[0].SQL, payload) {
		t.Fatalf("observer SQL leaked payload: %q", events[0].SQL)
	}

	deleted, err := query.ExecDelete(ctx, executor, query.Delete(OrderORM.Table).Where(OrderORM.ID.Eq(21)))
	if err != nil || deleted.RowsAffected != 1 {
		t.Fatalf("delete result=%#v err=%v", deleted, err)
	}
}

func verifyChecksumDriftRecovery(t *testing.T, ctx context.Context, ormBinary, databaseURL, migrationsDirectory string) {
	t.Helper()
	path := filepath.Join(migrationsDirectory, upgradeMigrationID, "up.sql")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(append([]byte(nil), original...), []byte("-- drift\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	runDatabaseCLI(t, ctx, ormBinary, databaseURL, migrationsDirectory, 1, "status")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	runDatabaseCLI(t, ctx, ormBinary, databaseURL, migrationsDirectory, 0, "status")
}

func verifyHistoryRecovery(t *testing.T, ctx context.Context, ormBinary, databaseURL, migrationsDirectory string) {
	t.Helper()
	original := filepath.Join(migrationsDirectory, initialMigrationID)
	hidden := filepath.Join(filepath.Dir(migrationsDirectory), initialMigrationID+".hidden")
	if err := os.Rename(original, hidden); err != nil {
		t.Fatal(err)
	}
	runDatabaseCLI(t, ctx, ormBinary, databaseURL, migrationsDirectory, 1, "status")
	if err := os.Rename(hidden, original); err != nil {
		t.Fatal(err)
	}
	runDatabaseCLI(t, ctx, ormBinary, databaseURL, migrationsDirectory, 0, "status")
}

func verifyDestructiveGate(t *testing.T, ctx context.Context, database *sql.DB, ormBinary, databaseURL, migrationsDirectory, destructivePath string) {
	t.Helper()
	runCLI(t, ctx, ormBinary, 1,
		"diff", "--after", destructivePath, "--id", destructiveMigrationID,
		"--migrations", migrationsDirectory, "--max-risk", "safe",
	)
	runCLI(t, ctx, ormBinary, 0,
		"diff", "--after", destructivePath, "--id", destructiveMigrationID,
		"--migrations", migrationsDirectory, "--max-risk", "destructive",
	)
	runDatabaseCLI(t, ctx, ormBinary, databaseURL, migrationsDirectory, 2,
		"apply", "--max-risk", "destructive",
	)
	assertColumnExists(t, ctx, database, "accounts", "legacy_note", true)
}

func runDatabaseCLI(t *testing.T, ctx context.Context, ormBinary, databaseURL, migrationsDirectory string, expectedExit int, command string, args ...string) {
	t.Helper()
	full := []string{command,
		"--database-url", databaseURL,
		"--migrations", migrationsDirectory,
		"--history-schema", "public",
		"--history-table", rcHistoryTable,
		"--lock-key", fmt.Sprint(rcLockKey),
		"--timeout", "30s",
	}
	full = append(full, args...)
	runCLI(t, ctx, ormBinary, expectedExit, full...)
}

func runCLI(t *testing.T, ctx context.Context, ormBinary string, expectedExit int, args ...string) (string, string) {
	t.Helper()
	command := exec.CommandContext(ctx, ormBinary, args...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	exit := 0
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			exit = exitError.ExitCode()
		} else {
			t.Fatalf("run orm %v: %v", args, err)
		}
	}
	if exit != expectedExit {
		t.Fatalf("orm %v exit=%d want=%d stdout=%s stderr=%s", args, exit, expectedExit, stdout.String(), stderr.String())
	}
	return stdout.String(), stderr.String()
}

func writeLoadedSnapshot(t *testing.T, directory, name string, load func() (migration.Snapshot, error)) string {
	t.Helper()
	snapshot, err := load()
	if err != nil {
		t.Fatal(err)
	}
	data, err := snapshot.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeRenames(t *testing.T, directory string, options migration.DiffOptions) string {
	t.Helper()
	data, err := json.MarshalIndent(struct {
		Version int                `json:"version"`
		Renames []migration.Rename `json:"renames"`
	}{Version: 1, Renames: options.Renames}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	path := filepath.Join(directory, "renames.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func packageDirectory(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve rcapp package directory")
	}
	return filepath.Dir(file)
}

func resetDatabase(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	if ctx == nil || ctx.Err() != nil {
		ctx = context.Background()
	}
	session, err := sevlumenpostgres.Acquire(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if _, err := session.ExecScriptContext(ctx, `DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatal(err)
	}
}

func mustExec(t *testing.T, ctx context.Context, database *sql.DB, statement string, args ...any) {
	t.Helper()
	if _, err := database.ExecContext(ctx, statement, args...); err != nil {
		t.Fatal(err)
	}
}

func assertRelationExists(t *testing.T, ctx context.Context, database *sql.DB, name string, expected bool) {
	t.Helper()
	var exists bool
	if err := database.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, "public."+name).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists != expected {
		t.Fatalf("relation %s exists=%t want=%t", name, exists, expected)
	}
}

func assertColumnExists(t *testing.T, ctx context.Context, database *sql.DB, table, column string, expected bool) {
	t.Helper()
	var exists bool
	if err := database.QueryRowContext(ctx, `SELECT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2
    )`, table, column).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists != expected {
		t.Fatalf("column %s.%s exists=%t want=%t", table, column, exists, expected)
	}
}

func assertLegacyData(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	var email, note string
	var accountID int64
	if err := database.QueryRowContext(ctx, `SELECT login_email, legacy_note FROM accounts WHERE id = 1`).Scan(&email, &note); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `SELECT account_id FROM orders WHERE id = 1`).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	if email != "legacy@example.test" || note != "preserve-me" || accountID != 1 {
		t.Fatalf("legacy data email=%q note=%q accountID=%d", email, note, accountID)
	}
}
