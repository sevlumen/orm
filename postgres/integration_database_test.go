package postgres

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	sevlumenpostgres "github.com/sevlumen/postgres"
)

func openIntegrationDatabase(t *testing.T) *sql.DB {
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

func executeIntegrationScript(t *testing.T, ctx context.Context, database *sql.DB, script string) {
	t.Helper()
	session, err := sevlumenpostgres.Acquire(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if _, err := session.ExecScriptContext(ctx, script); err != nil {
		t.Fatal(err)
	}
}

func quoteIdentifier(parts ...string) string {
	quoted := make([]string, len(parts))
	for index, part := range parts {
		quoted[index] = `"` + strings.ReplaceAll(part, `"`, `""`) + `"`
	}
	return strings.Join(quoted, ".")
}
