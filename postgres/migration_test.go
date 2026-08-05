package postgres

import (
	"testing"

	"github.com/sevlumen/orm/migration"
	"github.com/sevlumen/orm/schema"
)

func TestRenderMigration(t *testing.T) {
	t.Parallel()

	before, err := migration.NewSnapshot(schema.Schema{Tables: []schema.Table{{
		Name:    "users",
		Columns: []schema.Column{{Name: "id", Type: "uuid", PrimaryKey: true}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	after, err := migration.NewSnapshot(schema.Schema{Tables: []schema.Table{{
		Name: "users",
		Columns: []schema.Column{
			{Name: "id", Type: "uuid", PrimaryKey: true},
			{Name: "created_at", Type: "timestamptz", Default: "now()"},
		},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := migration.Diff(before, after)
	if err != nil {
		t.Fatal(err)
	}
	result, err := RenderMigration(plan)
	if err != nil {
		t.Fatal(err)
	}
	wantUp := "ALTER TABLE \"users\" ADD COLUMN \"created_at\" timestamptz NOT NULL DEFAULT now();\n"
	wantDown := "ALTER TABLE \"users\" DROP COLUMN \"created_at\";\n"
	if result.Up != wantUp || result.Down != wantDown {
		t.Fatalf("unexpected migration\nup:\n%s\ndown:\n%s", result.Up, result.Down)
	}
}

func TestRenderMigrationRejectsMalformedPlan(t *testing.T) {
	t.Parallel()
	plan := migration.Plan{Operations: []migration.Operation{{Kind: migration.CreateTable, Table: "users"}}}
	if _, err := RenderMigration(plan); err == nil {
		t.Fatal("expected invalid plan error")
	}
}
