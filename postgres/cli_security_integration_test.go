package postgres_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	sevlumenpostgres "github.com/sevlumen/postgres"
)

func TestORMCLIRejectsInjectedHistoryIdentifier(t *testing.T) {
	connectionString := os.Getenv("SEVLUMEN_TEST_DATABASE_URL")
	if connectionString == "" {
		t.Skip("SEVLUMEN_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	database, err := sevlumenpostgres.Open(connectionString)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.PingContext(ctx); err != nil {
		t.Fatal(err)
	}

	suffix := fmt.Sprintf("%x", time.Now().UnixNano())
	sentinel := "sl_cli_identifier_sentinel_" + suffix
	maliciousHistory := `history"; DROP TABLE ` + sentinel + `; --`
	defer cleanupCLIDatabase(database, sentinel, maliciousHistory)
	if _, err := database.ExecContext(ctx, "CREATE TABLE "+quoteCLIIdentifier(sentinel)+" (id bigint PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}

	configPath := writeCLIConfig(t, t.TempDir(), maliciousHistory, 81101, "safe", "5s")
	exit, stdout, stderr := runCLI(ctx, connectionString, "status", "--config", configPath, "--json")
	if exit != 1 {
		t.Fatalf("status exit=%d stdout=%s stderr=%s", exit, stdout, stderr)
	}
	if !strings.Contains(stderr, "invalid history table") {
		t.Fatalf("status did not reject injected identifier explicitly: %s", stderr)
	}
	assertInjectionTableExists(t, ctx, database, sentinel)

	var maliciousExists bool
	const catalogQuery = `SELECT EXISTS (
		SELECT 1
		FROM pg_class AS c
		JOIN pg_namespace AS n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relname = $1
	)`
	if err := database.QueryRowContext(ctx, catalogQuery, maliciousHistory).Scan(&maliciousExists); err != nil {
		t.Fatal(err)
	}
	if maliciousExists {
		t.Fatalf("malicious history identifier %q was created", maliciousHistory)
	}
}
