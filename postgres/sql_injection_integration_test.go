package postgres_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sevlumen/orm/postgres/query"
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
	pool, err := pgxpool.New(ctx, connectionString)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	suffix := fmt.Sprintf("%x", time.Now().UnixNano())
	tableName := "sl_injection_values_" + suffix
	sentinelName := "sl_injection_sentinel_" + suffix
	defer dropInjectionTable(pool, tableName)
	defer dropInjectionTable(pool, sentinelName)

	if _, err := pool.Exec(ctx, "CREATE TABLE "+pgx.Identifier{tableName}.Sanitize()+" (id bigint PRIMARY KEY, email text NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "CREATE TABLE "+pgx.Identifier{sentinelName}.Sanitize()+" (id bigint PRIMARY KEY)"); err != nil {
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
		if _, err := pool.Exec(ctx, "INSERT INTO "+pgx.Identifier{tableName}.Sanitize()+" (id, email) VALUES ($1, $2)", id, payload); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, "INSERT INTO "+pgx.Identifier{tableName}.Sanitize()+" (id, email) VALUES ($1, $2)", int64(999), "ordinary@example.com"); err != nil {
		t.Fatal(err)
	}

	for index, payload := range payloads {
		statement, err := query.Select(table).Where(email.Eq(payload)).Build()
		if err != nil {
			t.Fatal(err)
		}
		rows, err := pool.Query(ctx, statement.SQL, statement.Args...)
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
		assertInjectionTableExists(t, ctx, pool, sentinelName)
	}

	tautology, err := query.Select(table).Where(email.Eq(`missing' OR 1=1 --`)).Build()
	if err != nil {
		t.Fatal(err)
	}
	var count int
	rows, err := pool.Query(ctx, tautology.SQL, tautology.Args...)
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
	pool, err := pgxpool.New(ctx, connectionString)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	suffix := fmt.Sprintf("%x", time.Now().UnixNano())
	sentinelName := "sl_identifier_sentinel_" + suffix
	maliciousName := `sl_identifier_` + suffix + `"; DROP TABLE ` + sentinelName + `; --`
	defer dropInjectionTable(pool, maliciousName)
	defer dropInjectionTable(pool, sentinelName)
	if _, err := pool.Exec(ctx, "CREATE TABLE "+pgx.Identifier{sentinelName}.Sanitize()+" (id bigint PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "CREATE TABLE "+pgx.Identifier{maliciousName}.Sanitize()+" (id bigint PRIMARY KEY, email text NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO "+pgx.Identifier{maliciousName}.Sanitize()+" (id, email) VALUES (1, 'safe')"); err != nil {
		t.Fatal(err)
	}

	table, err := query.NewTable[injectionRecord](maliciousName, []string{"id", "email"}, func(row query.RowScanner) (injectionRecord, error) {
		var value injectionRecord
		err := row.Scan(&value.ID, &value.Email)
		return value, err
	})
	if err != nil {
		assertInjectionTableExists(t, ctx, pool, sentinelName)
		return
	}
	statement, err := query.Select(table).Build()
	if err != nil {
		t.Fatal(err)
	}
	rows, err := pool.Query(ctx, statement.SQL, statement.Args...)
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
	assertInjectionTableExists(t, ctx, pool, sentinelName)
}

func assertInjectionTableExists(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tableName string) {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", tableName).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatalf("sentinel table %q was removed", tableName)
	}
}

func dropInjectionTable(pool *pgxpool.Pool, tableName string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _ = pool.Exec(ctx, "DROP TABLE IF EXISTS "+pgx.Identifier{tableName}.Sanitize()+" CASCADE")
}
