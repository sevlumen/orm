package postgres

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sevlumen/orm/migration"
	"github.com/sevlumen/orm/schema"
)

func TestNativeMigrationAgainstPostgreSQL(t *testing.T) {
	connectionString := os.Getenv("SEVLUMEN_TEST_DATABASE_URL")
	if connectionString == "" {
		t.Skip("SEVLUMEN_TEST_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, connectionString)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	suffix := fmt.Sprintf("%x", time.Now().UnixNano())
	enumName := "sl_native_status_" + suffix
	tableName := "sl_native_orders_" + suffix
	model := schema.Schema{
		Extensions: []schema.Extension{{Name: "pgcrypto"}},
		Enums:      []schema.EnumType{{Name: enumName, Values: []string{"new", "paid"}}},
		Tables: []schema.Table{{
			Name: tableName,
			Columns: []schema.Column{
				{Name: "id", Type: "uuid", Default: "gen_random_uuid()", PrimaryKey: true},
				{Name: "status", Type: enumName},
				{Name: "payload", Type: "jsonb"},
				{Name: "tags", Type: "text[]"},
				{Name: "first_name", Type: "text"},
				{Name: "last_name", Type: "text"},
				{Name: "display_name", Type: "text", Generated: "first_name || ' ' || last_name"},
			},
		}},
	}

	after, err := migration.NewSnapshot(model)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := migration.Diff(migration.EmptySnapshot(), after)
	if err != nil {
		t.Fatal(err)
	}
	generated, err := RenderMigration(plan)
	if err != nil {
		t.Fatal(err)
	}

	qualifiedTable := pgx.Identifier{tableName}.Sanitize()
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, "DROP TABLE IF EXISTS "+qualifiedTable+" CASCADE")
		_, _ = pool.Exec(cleanupCtx, "DROP TYPE IF EXISTS "+pgx.Identifier{enumName}.Sanitize()+" CASCADE")
	}()

	executeNativeScript(t, ctx, pool, generated.Up)

	var displayName, payloadType, secondTag, status string
	insert := "INSERT INTO " + qualifiedTable + `
        (status, payload, tags, first_name, last_name)
        VALUES ('new', '{"source":"test"}'::jsonb, ARRAY['go', 'orm'], 'Ada', 'Lovelace')
        RETURNING display_name, pg_typeof(payload)::text, tags[2], status::text`
	insert = strings.ReplaceAll(insert, `\"`, `"`)
	if err := pool.QueryRow(ctx, insert).Scan(&displayName, &payloadType, &secondTag, &status); err != nil {
		t.Fatal(err)
	}
	if displayName != "Ada Lovelace" || payloadType != "jsonb" || secondTag != "orm" || status != "new" {
		t.Fatalf("unexpected native values: display=%q payload=%q tag=%q status=%q", displayName, payloadType, secondTag, status)
	}

	executeNativeScript(t, ctx, pool, generated.Down)
	var tableExists, enumExists bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", tableName).Scan(&tableExists); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_type WHERE typname = $1)`, enumName).Scan(&enumExists); err != nil {
		t.Fatal(err)
	}
	if tableExists || enumExists {
		t.Fatalf("rollback left native objects: table=%v enum=%v", tableExists, enumExists)
	}
	var extensionExists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pgcrypto')`).Scan(&extensionExists); err != nil {
		t.Fatal(err)
	}
	if !extensionExists {
		t.Fatal("rollback unexpectedly removed pgcrypto")
	}
}

func executeNativeScript(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string) {
	t.Helper()
	connection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	results, err := connection.Conn().PgConn().Exec(ctx, sql).ReadAll()
	connection.Release()
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range results {
		if result.Err != nil {
			t.Fatal(result.Err)
		}
	}
}
