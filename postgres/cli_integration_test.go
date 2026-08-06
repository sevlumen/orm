package postgres_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sevlumen/orm/internal/ormcli"
	"github.com/sevlumen/orm/migration"
	"github.com/sevlumen/orm/migration/artifact"
	pgdialect "github.com/sevlumen/orm/postgres"
	"github.com/sevlumen/orm/schema"
)

func TestORMCLIWorkflowAgainstPostgreSQL(t *testing.T) {
	connectionString := os.Getenv("SEVLUMEN_TEST_DATABASE_URL")
	if connectionString == "" {
		t.Skip("SEVLUMEN_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, connectionString)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	suffix := fmt.Sprintf("%x", time.Now().UnixNano())
	t.Run("apply status risk and rollback", func(t *testing.T) {
		tableName := "sl_cli_users_" + suffix
		historyTable := "sl_cli_history_" + suffix
		root := t.TempDir()
		before := migration.EmptySnapshot()
		first := cliSnapshot(t, schema.Schema{Tables: []schema.Table{{
			Name: tableName,
			Columns: []schema.Column{
				{Name: "id", Type: "bigint", PrimaryKey: true},
				{Name: "email", Type: "text"},
			},
		}}})
		writeCLIArtifact(t, root, "202608060001_init", before, first)
		second := cliSnapshot(t, schema.Schema{Tables: []schema.Table{{
			Name: tableName,
			Columns: []schema.Column{
				{Name: "id", Type: "bigint", PrimaryKey: true},
				{Name: "email", Type: "text"},
			},
			UniqueConstraints: []schema.UniqueConstraint{{Name: "uq_" + tableName + "_email", Columns: []string{"email"}}},
		}}})
		writeCLIArtifact(t, root, "202608060002_unique", first, second)
		configPath := writeCLIConfig(t, root, historyTable, 81001, "safe", "5s")
		defer cleanupCLIDatabase(pool, tableName, historyTable)

		exit, stdout, stderr := runCLI(ctx, connectionString, "status", "--config", configPath, "--json")
		if exit != 0 || !strings.Contains(stdout, `"pending":["202608060001_init","202608060002_unique"]`) {
			t.Fatalf("initial status exit=%d stdout=%s stderr=%s", exit, stdout, stderr)
		}

		exit, _, stderr = runCLI(ctx, connectionString, "apply", "--config", configPath)
		if exit != 1 || !strings.Contains(stderr, "exceeds configured maximum") {
			t.Fatalf("safe apply exit=%d stderr=%s", exit, stderr)
		}
		var applied int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{"public", historyTable}.Sanitize()).Scan(&applied); err != nil {
			t.Fatal(err)
		}
		if applied != 1 {
			t.Fatalf("applied history rows=%d, want 1 safe migration", applied)
		}

		exit, stdout, stderr = runCLI(ctx, connectionString, "apply", "--config", configPath, "--max-risk", "review", "--json")
		if exit != 0 || !strings.Contains(stdout, `"count":1`) {
			t.Fatalf("review apply exit=%d stdout=%s stderr=%s", exit, stdout, stderr)
		}

		exit, _, _ = runCLI(ctx, connectionString, "rollback", "--config", configPath, "--steps", "2")
		if exit != 2 {
			t.Fatalf("unconfirmed rollback exit=%d, want 2", exit)
		}
		exit, stdout, stderr = runCLI(ctx, connectionString, "rollback", "--config", configPath, "--steps", "2", "--yes", "--json")
		if exit != 0 || !strings.Contains(stdout, `"count":2`) {
			t.Fatalf("rollback exit=%d stdout=%s stderr=%s", exit, stdout, stderr)
		}
		var exists bool
		if err := pool.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", tableName).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Fatal("rollback left application table")
		}
	})

	t.Run("checksum drift", func(t *testing.T) {
		tableName := "sl_cli_drift_" + suffix
		historyTable := "sl_cli_drift_history_" + suffix
		root := t.TempDir()
		after := cliSnapshot(t, schema.Schema{Tables: []schema.Table{{
			Name: tableName,
			Columns: []schema.Column{{Name: "id", Type: "bigint", PrimaryKey: true}},
		}}})
		id := "202608060001_init"
		writeCLIArtifact(t, root, id, migration.EmptySnapshot(), after)
		configPath := writeCLIConfig(t, root, historyTable, 81002, "safe", "5s")
		defer cleanupCLIDatabase(pool, tableName, historyTable)
		if exit, _, stderr := runCLI(ctx, connectionString, "apply", "--config", configPath); exit != 0 {
			t.Fatalf("apply exit=%d stderr=%s", exit, stderr)
		}
		if err := os.WriteFile(filepath.Join(root, id, artifact.UpFilename), []byte("SELECT 1;\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		exit, _, stderr := runCLI(ctx, connectionString, "status", "--config", configPath)
		if exit != 1 || !strings.Contains(stderr, "checksum") {
			t.Fatalf("drift status exit=%d stderr=%s", exit, stderr)
		}
	})

	t.Run("advisory lock cancellation", func(t *testing.T) {
		historyTable := "sl_cli_lock_history_" + suffix
		root := t.TempDir()
		const lockKey int64 = 81003
		configPath := writeCLIConfig(t, root, historyTable, lockKey, "safe", "80ms")
		defer cleanupCLIDatabase(pool, "", historyTable)
		connection, err := pool.Acquire(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer connection.Release()
		if _, err := connection.Exec(ctx, "SELECT pg_advisory_lock($1)", lockKey); err != nil {
			t.Fatal(err)
		}
		defer connection.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", lockKey)

		exit, _, stderr := runCLI(ctx, connectionString, "status", "--config", configPath)
		if exit != 1 || (!strings.Contains(stderr, "deadline") && !strings.Contains(stderr, "canceled")) {
			t.Fatalf("locked status exit=%d stderr=%s", exit, stderr)
		}
	})
}

func runCLI(ctx context.Context, databaseURL string, args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	app := ormcli.New()
	app.Out, app.Err = &stdout, &stderr
	app.LookupEnv = func(name string) (string, bool) {
		if name == "CLI_DATABASE_URL" {
			return databaseURL, true
		}
		return "", false
	}
	exit := app.Run(ctx, args)
	return exit, stdout.String(), stderr.String()
}

func writeCLIConfig(t *testing.T, migrationsRoot, historyTable string, lockKey int64, maximumRisk, timeout string) string {
	t.Helper()
	content := fmt.Sprintf(`{
  "version": 1,
  "migrations": {
    "directory": %q,
    "databaseEnv": "CLI_DATABASE_URL",
    "historySchema": "public",
    "historyTable": %q,
    "lockKey": %d,
    "maximumRisk": %q,
    "timeout": %q
  }
}
`, migrationsRoot, historyTable, lockKey, maximumRisk, timeout)
	path := filepath.Join(t.TempDir(), "orm.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func cliSnapshot(t *testing.T, model schema.Schema) migration.Snapshot {
	t.Helper()
	snapshot, err := migration.NewSnapshot(model)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func writeCLIArtifact(t *testing.T, root, id string, before, after migration.Snapshot) {
	t.Helper()
	plan, err := migration.Diff(before, after)
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := pgdialect.RenderMigration(plan)
	if err != nil {
		t.Fatal(err)
	}
	built, err := artifact.Build(id, rendered, after)
	if err != nil {
		t.Fatal(err)
	}
	if err := artifact.Write(root, built); err != nil {
		t.Fatal(err)
	}
}

func cleanupCLIDatabase(pool *pgxpool.Pool, tableName, historyTable string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if tableName != "" {
		_, _ = pool.Exec(ctx, "DROP TABLE IF EXISTS "+pgx.Identifier{tableName}.Sanitize()+" CASCADE")
	}
	_, _ = pool.Exec(ctx, "DROP TABLE IF EXISTS "+pgx.Identifier{"public", historyTable}.Sanitize()+" CASCADE")
}
