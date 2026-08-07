package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sevlumen/orm/migration"
	"github.com/sevlumen/orm/schema"
)

func TestNativeMigrationAgainstPostgreSQL(t *testing.T) {
	database := openIntegrationDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

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

	qualifiedTable := quoteIdentifier(tableName)
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = database.ExecContext(cleanupCtx, "DROP TABLE IF EXISTS "+qualifiedTable+" CASCADE")
		_, _ = database.ExecContext(cleanupCtx, "DROP TYPE IF EXISTS "+quoteIdentifier(enumName)+" CASCADE")
	}()

	executeIntegrationScript(t, ctx, database, generated.Up)

	var displayName, payloadType, secondTag, status string
	insert := "INSERT INTO " + qualifiedTable + `
        (status, payload, tags, first_name, last_name)
        VALUES ('new', '{"source":"test"}'::jsonb, ARRAY['go', 'orm'], 'Ada', 'Lovelace')
        RETURNING display_name, pg_typeof(payload)::text, tags[2], status::text`
	if err := database.QueryRowContext(ctx, insert).Scan(&displayName, &payloadType, &secondTag, &status); err != nil {
		t.Fatal(err)
	}
	if displayName != "Ada Lovelace" || payloadType != "jsonb" || secondTag != "orm" || status != "new" {
		t.Fatalf("unexpected native values: display=%q payload=%q tag=%q status=%q", displayName, payloadType, secondTag, status)
	}

	executeIntegrationScript(t, ctx, database, generated.Down)
	var tableExists, enumExists bool
	if err := database.QueryRowContext(ctx, "SELECT to_regclass($1) IS NOT NULL", tableName).Scan(&tableExists); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_type WHERE typname = $1)`, enumName).Scan(&enumExists); err != nil {
		t.Fatal(err)
	}
	if tableExists || enumExists {
		t.Fatalf("rollback left native objects: table=%v enum=%v", tableExists, enumExists)
	}
	var extensionExists bool
	if err := database.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pgcrypto')`).Scan(&extensionExists); err != nil {
		t.Fatal(err)
	}
	if !extensionExists {
		t.Fatal("rollback unexpectedly removed pgcrypto")
	}
}
