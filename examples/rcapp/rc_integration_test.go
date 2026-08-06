package rcapp

import (
	"bytes"
	"context"
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

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sevlumen/orm/migration"
	"github.com/sevlumen/orm/postgres/query"
)

const (
	initialMigrationID     = "20260806010000_initial_schema"
	safeMigrationID        = "20260806020000_add_legacy_note"
	upgradeMigrationID     = "20260806030000_upgrade_accounts"
	destructiveMigrationID = "20260806040000_drop_legacy_note"
)

func TestReleaseCandidateWorkflow(t *testing.T) {
	databaseURL := os.Getenv("SEVLUMEN_TEST_DATABASE_URL")
	ormBinary := os.Getenv("SEVLUMEN_RC_ORM_BINARY")
	if databaseURL == "" || ormBinary == "" {
		t.Skip("set SEVLUMEN_TEST_DATABASE_URL and SEVLUMEN_RC_ORM_BINARY to run the RC application")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	resetDatabase(t, ctx, pool)

	exampleDirectory := packageDirectory(t)
	runCLI(t, ctx, ormBinary, 0,
		"generate", "--dir", exampleDirectory, "--output", "orm_gen.go",
		"--type", "Account", "--type", "Order", "--check",
	)

	workspace := t.TempDir()
	migrationsDirectory := filepath.Join(workspace, "migrations")
	initialPath := writeSnapshot(t, workspace, "initial.snapshot.json", mustSnapshot(t, InitialSnapshot()))
	safePath := writeSnapshot(t, workspace, "safe.snapshot.json", mustSnapshot(t, SafeSnapshot()))
	finalPath := writeSnapshot(t, workspace, "final.snapshot.json", mustSnapshot(t, FinalSnapshot()))
	destructivePath := writeSnapshot(t, workspace, "destructive.snapshot.json", mustSnapshot(t, DestructiveSnapshot()))
	renamesPath := writeRenames(t, workspace, UpgradeRenames())

	// Fresh database: create and apply the initial review-risk schema.
	runCLI(t, ctx, ormBinary, 0,
		"diff", "--after", initialPath, "--id", initialMigrationID,
		"--migrations", migrationsDirectory, "--max-risk", "review",
	)
	runCLI(t, ctx, ormBinary, 0, "validate", "--migrations", migrationsDirectory)
	runDatabaseCLI(t, ctx, ormBinary, databaseURL, migrationsDirectory, 0,
		"apply", "--max-risk", "review",
	)

	if _, err := pool.Exec(ctx, `INSERT INTO users (id, email, active) VALUES (1, $1, true)`, "legacy@example.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO orders (id, user_id, total) VALUES (1, 1, 1200)`); err != nil {
		t.Fatal(err)
	}

	// Safe migration on a populated earlier schema.
	runCLI(t, ctx, ormBinary, 0,
		"diff", "--after", safePath, "--id", safeMigrationID,
		"--migrations", migrationsDirectory, "--max-risk", "safe",
	)
	runDatabaseCLI(t, ctx, ormBinary, databaseURL, migrationsDirectory, 0,
		"apply", "--max-risk", "safe",
	)
	if _, err := pool.Exec(ctx, `UPDATE users SET legacy_note = $1 WHERE id = 1`, "preserve-me"); err != nil {
		t.Fatal(err)
	}

	// Explicit rename and review-risk upgrade. The safe gate must refuse it.
	runCLI(t, ctx, ormBinary, 0,
		"diff", "--after", finalPath, "--id", upgradeMigrationID,
		"--renames", renamesPath, "--migrations", migrationsDirectory,
		"--max-risk", "review",
	)
	runDatabaseCLI(t, ctx, ormBinary, databaseURL, migrationsDirectory, 1,
		"apply", "--max-risk", "safe",
	)
	assertRelationExists(t, ctx, pool, "users", true)
	assertRelationExists(t, ctx, pool, "accounts", false)

	runDatabaseCLI(t, ctx, ormBinary, databaseURL, migrationsDirectory, 0,
		"apply", "--max-risk", "review",
	)
	runDatabaseCLI(t, ctx, ormBinary, databaseURL, migrationsDirectory, 0, "status")
	assertRelationExists(t, ctx, pool, "users", false)
	assertRelationExists(t, ctx, pool, "accounts", true)
	assertColumnExists(t, ctx, pool, "accounts", "login_email", true)
	assertColumnExists(t, ctx, pool, "orders", "account_id", true)
	assertLegacyData(t, ctx, pool)

	exerciseTypedRuntime(t, ctx, pool)

	// Reversible rollback restores the earlier schema shape and preserves rows.
	runDatabaseCLI(t, ctx, ormBinary, databaseURL, migrationsDirectory, 0,
		"rollback", "--steps", "1", "--yes",
	)
	assertRelationExists(t, ctx, pool, "accounts", false)
	assertRelationExists(t, ctx, pool, "users", true)
	assertColumnExists(t, ctx, pool, "users", "email", true)
	assertColumnExists(t, ctx, pool, "users", "legacy_note", true)
	assertColumnExists(t, ctx, pool, "orders", "user_id", true)
	var preserved string
	if err := pool.QueryRow(ctx, `SELECT legacy_note FROM users WHERE id = 1`).Scan(&preserved); err != nil {
		t.Fatal(err)
	}
	if preserved != "preserve-me" {
		t.Fatalf("legacy note after rollback=%q", preserved)
	}

	runDatabaseCLI(t, ctx, ormBinary, databaseURL, migrationsDirectory, 0,
		"apply", "--max-risk", "review",
	)
	assertLegacyData(t, ctx, pool)

	// Checksum drift fails closed; restoring reviewed bytes recovers status.
	upgradeSQL := filepath.Join(migrationsDirectory, upgradeMigrationID, "up.sql")
	originalSQL, err := os.ReadFile(upgradeSQL)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(upgradeSQL, append(append([]byte(nil), originalSQL...), []byte("-- tampered\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	runDatabaseCLI(t, ctx, ormBinary, databaseURL, migrationsDirectory, 1, "status")
	if err := os.WriteFile(upgradeSQL, originalSQL, 0o644); err != nil {
		t.Fatal(err)
	}
	runDatabaseCLI(t, ctx, ormBinary, databaseURL, migrationsDirectory, 0, "status")

	// A non-prefix local history is rejected; restoring the missing artifact recovers.
	initialDirectory := filepath.Join(migrationsDirectory, initialMigrationID)
	initialBackup := filepath.Join(workspace, initialMigrationID+".backup")
	if err := os.Rename(initialDirectory, initialBackup); err != nil {
		t.Fatal(err)
	}
	runDatabaseCLI(t, ctx, ormBinary, databaseURL, migrationsDirectory, 1, "status")
	if err := os.Rename(initialBackup, initialDirectory); err != nil {
		t.Fatal(err)
	}
	runDatabaseCLI(t, ctx, ormBinary, databaseURL, migrationsDirectory, 0, "status")

	// Destructive generation and execution require explicit gates and confirmation.
	runCLI(t, ctx, ormBinary, 1,
		"diff", "--after", destructivePath, "--id", destructiveMigrationID,
		"--migrations", migrationsDirectory, "--max-risk", "safe",
	)
	runCLI(t, ctx, ormBinary, 0,
		"diff", "--after", destructivePath, "--id", destructiveMigrationID,
		"--migrations", migrationsDirectory, "--max-risk", "destructive",
	)
	runDatabaseCLI(t, ctx, ormBinary, databaseURL, migrationsDirectory, 1,
		"apply", "--max-risk", "safe",
	)
	runDatabaseCLI(t, ctx, ormBinary, databaseURL, migrationsDirectory, 2,
		"apply", "--max-risk", "destructive",
	)
	assertColumnExists(t, ctx, pool, "accounts", "legacy_note", true)
	if err := os.RemoveAll(filepath.Join(migrationsDirectory, destructiveMigrationID)); err != nil {
		t.Fatal(err)
	}
	runDatabaseCLI(t, ctx, ormBinary, databaseURL, migrationsDirectory, 0, "status")
}

func exerciseTypedRuntime(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	recorder := &Recorder{}
	executor, err := NewExecutor(pool, recorder)
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
	if created.LoginEmail != payload {
		t.Fatalf("created payload=%q", created.LoginEmail)
	}
	matched, found, err := FindAccountByEmail(ctx, executor, payload)
	if err != nil || !found || matched.ID != 2 {
		t.Fatalf("payload lookup account=%#v found=%t err=%v", matched, found, err)
	}
	assertRelationExists(t, ctx, pool, "accounts", true)

	if err := CreateAccountAndOrder(ctx, pool, recorder,
		Account{ID: 3, LoginEmail: "transaction@example.test", Active: true},
		Order{ID: 3, AccountID: 3, Total: 3400, Status: "paid"},
	); err != nil {
		t.Fatal(err)
	}
	if err := CreateAccountAndOrder(ctx, pool, recorder,
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

	deleted, err := query.DeleteOne(ctx, executor, query.Delete(OrderORM.Table).Where(OrderORM.ID.Eq(21)))
	if err != nil || deleted.ID != 21 {
		t.Fatalf("deleted order=%#v err=%v", deleted, err)
	}

	for _, event := range recorder.Events() {
		if strings.Contains(event.SQL, payload) {
			t.Fatalf("observer event leaked argument: %#v", event)
		}
	}
}

func runDatabaseCLI(t *testing.T, ctx context.Context, binary, databaseURL, migrations string, expectedExit int, command string, arguments ...string) commandResult {
	t.Helper()
	args := []string{command, "--database-url", databaseURL, "--migrations", migrations,
		"--history-schema", "public", "--history-table", "__sevlumen_rc_migrations",
		"--lock-key", "9106001", "--timeout", "30s"}
	args = append(args, arguments...)
	return runCLI(t, ctx, binary, expectedExit, args...)
}

type commandResult struct {
	stdout string
	stderr string
}

func runCLI(t *testing.T, parent context.Context, binary string, expectedExit int, arguments ...string) commandResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(parent, 45*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, arguments...)
	command.Env = os.Environ()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	exitCode := 0
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			exitCode = exitError.ExitCode()
		} else {
			t.Fatalf("run %s %s: %v", binary, strings.Join(arguments, " "), err)
		}
	}
	if exitCode != expectedExit {
		t.Fatalf("%s %s exit=%d want=%d\nstdout:\n%s\nstderr:\n%s", binary, strings.Join(arguments, " "), exitCode, expectedExit, stdout.String(), stderr.String())
	}
	return commandResult{stdout: stdout.String(), stderr: stderr.String()}
}

func resetDatabase(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `DROP SCHEMA IF EXISTS public CASCADE`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `CREATE SCHEMA public`); err != nil {
		t.Fatal(err)
	}
}

func packageDirectory(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve rcapp source directory")
	}
	return filepath.Dir(filename)
}

func mustSnapshot(t *testing.T, snapshot migration.Snapshot, err error) migration.Snapshot {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func writeSnapshot(t *testing.T, directory, name string, snapshot migration.Snapshot) string {
	t.Helper()
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
	payload := struct {
		Version int                `json:"version"`
		Renames []migration.Rename `json:"renames"`
	}{Version: 1, Renames: options.Renames}
	data, err := json.MarshalIndent(payload, "", "  ")
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

func assertLegacyData(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var email string
	var note *string
	if err := pool.QueryRow(ctx, `SELECT login_email, legacy_note FROM accounts WHERE id = 1`).Scan(&email, &note); err != nil {
		t.Fatal(err)
	}
	if email != "legacy@example.test" || note == nil || *note != "preserve-me" {
		t.Fatalf("upgraded data email=%q note=%v", email, note)
	}
	var accountID int64
	var total int64
	if err := pool.QueryRow(ctx, `SELECT account_id, total FROM orders WHERE id = 1`).Scan(&accountID, &total); err != nil {
		t.Fatal(err)
	}
	if accountID != 1 || total != 1200 {
		t.Fatalf("upgraded order account=%d total=%d", accountID, total)
	}
}

func assertRelationExists(t *testing.T, ctx context.Context, pool *pgxpool.Pool, relation string, expected bool) {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relname = $1
	)`, relation).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists != expected {
		t.Fatalf("relation %q exists=%t want=%t", relation, exists, expected)
	}
}

func assertColumnExists(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table, column string, expected bool) {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2
	)`, table, column).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists != expected {
		t.Fatalf("column %s.%s exists=%t want=%t", table, column, exists, expected)
	}
}

func Example_releaseCandidateWorkflow() {
	fmt.Println("generate -> diff -> validate -> apply -> typed runtime -> rollback -> recovery")
	// Output: generate -> diff -> validate -> apply -> typed runtime -> rollback -> recovery
}
