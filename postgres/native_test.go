package postgres

import (
	"strings"
	"testing"

	"github.com/sevlumen/orm/migration"
	"github.com/sevlumen/orm/schema"
)

func TestRenderCreateSchemaOrdersAndQuotesNativeObjects(t *testing.T) {
	t.Parallel()

	model := schema.Schema{
		Extensions: []schema.Extension{{Name: "pgcrypto"}},
		Enums:      []schema.EnumType{{Name: "order_status", Values: []string{"new", "customer's choice"}}},
		Tables: []schema.Table{{
			Name: "orders",
			Columns: []schema.Column{
				{Name: "id", Type: "uuid", Default: "gen_random_uuid()", PrimaryKey: true},
				{Name: "status", Type: "order_status"},
				{Name: "payload", Type: "jsonb"},
				{Name: "tags", Type: "text[]"},
				{Name: "subtotal", Type: "integer"},
				{Name: "tax", Type: "integer"},
				{Name: "total", Type: "integer", Generated: "subtotal + tax"},
			},
		}},
	}

	sql, err := RenderCreateSchema(model)
	if err != nil {
		t.Fatal(err)
	}
	extension := strings.Index(sql, `CREATE EXTENSION IF NOT EXISTS "pgcrypto";`)
	enum := strings.Index(sql, `CREATE TYPE "order_status" AS ENUM ('new', 'customer''s choice');`)
	table := strings.Index(sql, `CREATE TABLE "orders"`)
	generated := strings.Index(sql, `"total" integer GENERATED ALWAYS AS (subtotal + tax) STORED NOT NULL`)
	if extension < 0 || enum < 0 || table < 0 || generated < 0 || !(extension < enum && enum < table) {
		t.Fatalf("unexpected native SQL:\n%s", sql)
	}
}

func TestRenderMigrationNativeRollbackSemantics(t *testing.T) {
	t.Parallel()

	extension := schema.Extension{Name: "pgcrypto"}
	enum := schema.EnumType{Name: "order_status", Values: []string{"new", "paid"}}
	table := schema.Table{Name: "orders", Columns: []schema.Column{{Name: "status", Type: "order_status"}}}
	result, err := RenderMigration(migration.Plan{Operations: []migration.Operation{
		{Kind: migration.CreateTable, Table: table.Name, AfterTable: &table},
		{Kind: migration.CreateEnum, AfterEnum: &enum},
		{Kind: migration.CreateExtension, AfterExtension: &extension, Risk: migration.RiskReview},
	}})
	if err != nil {
		t.Fatal(err)
	}

	upExtension := strings.Index(result.Up, `CREATE EXTENSION IF NOT EXISTS "pgcrypto";`)
	upEnum := strings.Index(result.Up, `CREATE TYPE "order_status"`)
	upTable := strings.Index(result.Up, `CREATE TABLE "orders"`)
	if !(upExtension >= 0 && upExtension < upEnum && upEnum < upTable) {
		t.Fatalf("unexpected up ordering:\n%s", result.Up)
	}

	downTable := strings.Index(result.Down, `DROP TABLE "orders";`)
	downEnum := strings.Index(result.Down, `DROP TYPE "order_status";`)
	downExtension := strings.Index(result.Down, `-- extension "pgcrypto" retained during rollback`)
	if !(downTable >= 0 && downTable < downEnum && downEnum < downExtension) {
		t.Fatalf("unexpected down ordering:\n%s", result.Down)
	}
}

func TestRenderMigrationRecreatesDroppedEnumBeforeTables(t *testing.T) {
	t.Parallel()

	enum := schema.EnumType{Name: "order_status", Values: []string{"new", "paid"}}
	table := schema.Table{Name: "orders", Columns: []schema.Column{{Name: "status", Type: "order_status"}}}
	result, err := RenderMigration(migration.Plan{Operations: []migration.Operation{
		{Kind: migration.DropTable, Table: table.Name, BeforeTable: &table, Risk: migration.RiskDestructive},
		{Kind: migration.DropEnum, BeforeEnum: &enum, Risk: migration.RiskDestructive},
	}})
	if err != nil {
		t.Fatal(err)
	}
	createEnum := strings.Index(result.Down, `CREATE TYPE "order_status"`)
	createTable := strings.Index(result.Down, `CREATE TABLE "orders"`)
	if createEnum < 0 || createTable < 0 || createEnum > createTable {
		t.Fatalf("down SQL recreates table before enum:\n%s", result.Down)
	}
}
