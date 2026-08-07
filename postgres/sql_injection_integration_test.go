package postgres_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/sevlumen/orm/postgres/query"
	sevlumenpostgres "github.com/sevlumen/postgres"
)

type injectionRecord struct {
	ID    int64
	Email string
}

func TestTypedQueriesResistSQLInjectionAgainstPostgreSQL(t *testing.T) {
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
	tableName := "sl_injection_values_" + suffix
	sentinelName := "sl_injection_sentinel_" + suffix
	defer dropInjectionTable(database, tableName)
	defer dropInjectionTable(database, sentinelName)

	if _, err := database.ExecContext(ctx, "CREATE TABLE "+quoteCLIIdentifier(tableName)+" (id bigint PRIMARY KEY, email text NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, "CREATE TABLE "+quoteCLIIdentifier(sentinelName)+" (id bigint PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}

	table, err := query.NewTable[injectionRecord](tableName, []string{"id", "email"}, func(row query.RowScanner) (injectionRecord, error) {
		var value injectionRecord
		err := row.Scan(&value.ID, &value.Email)
		return value, err
	})
	if err != nil {
		t.Fatal(err)
	}
	email, err := query.NewColumn[injectionRecord, string](table, "email")
	if err != nil {
		t.Fatal(err)
	}

	payloads := []string{
		`' OR 1=1 --`,
		`'; DROP TABLE ` + sentinelName + `; --`,
		`' UNION SELECT 999, current_user --`,
		`admin'/*`,
		`\'; SELECT pg_sleep(10); --`,
	}
	for index, payload := range payloads {
		id := int64(index + 1)
		if _, err := database.ExecContext(ctx, "INSERT INTO "+quoteCLIIdentifier(tableName)+" (id, email) VALUES ($1, $2)", id, payload); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.ExecContext(ctx, "INSERT INTO "+quoteCLIIdentifier(tableName)+" (id, email) VALUES ($1, $2)", int64(999), "ordinary@example.com"); err != nil {
		t.Fatal(err)
	}

	for index, payload := range payloads {
		statement, err := query.Select(table).Where(email.Eq(payload)).Build()
		if err != nil {
			t.Fatal(err)
		}
		rows, err := database.QueryContext(ctx, statement.SQL, statement.Args...)
		if err != nil {
			t.Fatalf("execute payload %q: %v", payload, err)
		}
		var values []injectionRecord
		for rows.Next() {
			value, err := table.Scan(rows)
			if err != nil {
				rows.Close()
				t.Fatal(err)
			}
			values = append(values, value)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		rows.Close()
		if len(values) != 1 || values[0].ID != int64(index+1) || values[0].Email != payload {
			t.Fatalf("payload %q returned %#v, want exactly its stored row", payload, values)
		}
		assertInjectionTableExists(t, ctx, database, sentinelName)
	}

	tautology, err := query.Select(table).Where(email.Eq(`missing' OR 1=1 --`)).Build()
	if err != nil {
		t.Fatal(err)
	}
	var count int
	rows, err := database.QueryContext(ctx, tautology.SQL, tautology.Args...)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		count++
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		t.Fatal(err)
	}
	rows.Close()
	if count != 0 {
		t.Fatalf("tautology payload returned %d rows, want 0", count)
	}
}

func TestTypedTableIdentifierInjectionIsFailClosedOrQuoted(t *testing.T) {
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
	sentinelName := "sl_identifier_sentinel_" + suffix
	maliciousName := `sl_identifier_` + suffix + `"; DROP TABLE ` + sentinelName + `; --`
	defer dropInjectionTable(database, maliciousName)
	defer dropInjectionTable(database, sentinelName)
	if _, err := database.ExecContext(ctx, "CREATE TABLE "+quoteCLIIdentifier(sentinelName)+" (id bigint PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, "CREATE TABLE "+quoteCLIIdentifier(maliciousName)+" (id bigint PRIMARY KEY, email text NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, "INSERT INTO "+quoteCLIIdentifier(maliciousName)+" (id, email) VALUES (1, 'safe')"); err != nil {
		t.Fatal(err)
	}

	table, err := query.NewTable[injectionRecord](maliciousName, []string{"id", "email"}, func(row query.RowScanner) (injectionRecord, error) {
		var value injectionRecord
		err := row.Scan(&value.ID, &value.Email)
		return value, err
	})
	if err != nil {
		assertInjectionTableExists(t, ctx, database, sentinelName)
		return
	}
	statement, err := query.Select(table).Build()
	if err != nil {
		t.Fatal(err)
	}
	rows, err := database.QueryContext(ctx, statement.SQL, statement.Args...)
	if err != nil {
		t.Fatalf("quoted malicious identifier did not address its single table safely: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("quoted malicious identifier returned no row")
	}
	value, err := table.Scan(rows)
	if err != nil {
		t.Fatal(err)
	}
	if value.ID != 1 || value.Email != "safe" {
		t.Fatalf("unexpected row: %#v", value)
	}
	assertInjectionTableExists(t, ctx, database, sentinelName)
}

func assertInjectionTableExists(t *testing.T, ctx context.Context, database *sql.DB, tableName string) {
	t.Helper()
	var exists bool
	if err := database.QueryRowContext(ctx, "SELECT to_regclass($1) IS NOT NULL", tableName).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatalf("sentinel table %q was removed", tableName)
	}
}

func dropInjectionTable(database *sql.DB, tableName string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _ = database.ExecContext(ctx, "DROP TABLE IF EXISTS "+quoteCLIIdentifier(tableName)+" CASCADE")
}
