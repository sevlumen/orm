package query

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	postgresdriver "github.com/sevlumen/postgres"
)

func openIntegrationDatabase(t testing.TB) *sql.DB {
	t.Helper()
	connectionString := os.Getenv("SEVLUMEN_TEST_DATABASE_URL")
	if connectionString == "" {
		t.Skip("SEVLUMEN_TEST_DATABASE_URL is not set")
	}
	db, err := postgresdriver.Open(connectionString)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(16)
	db.SetMaxIdleConns(8)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
