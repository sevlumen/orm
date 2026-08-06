package postgres_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestORMCLIRejectsInjectedHistoryIdentifier(t *testing.T) {
	connectionString := os.Getenv("SEVLUMEN_TEST_DATABASE_URL")
	if connectionString == "" {
		t.Skip("SEVLUMEN_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, connectionString)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	suffix := fmt.Sprintf("%x", time.Now().UnixNano())
	sentinel := "sl_cli_identifier_sentinel_" + suffix
	maliciousHistory := `history"; DROP TABLE ` + sentinel + `; --`
	defer cleanupCLIDatabase(pool, sentinel, maliciousHistory)
	if _, err := pool.Exec(ctx, "CREATE TABLE "+pgx.Identifier{sentinel}.Sanitize()+" (id bigint PRIMARY KEY)"); err != nil {
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
	assertInjectionTableExists(t, ctx, pool, sentinel)

	var maliciousExists bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", maliciousHistory).Scan(&maliciousExists); err != nil {
		t.Fatal(err)
	}
	if maliciousExists {
		t.Fatalf("malicious history identifier %q was created", maliciousHistory)
	}
}
