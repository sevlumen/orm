package postgres

import (
	"strings"
	"testing"

	"github.com/sevlumen/orm/migration"
	"github.com/sevlumen/orm/schema"
)

func TestRenderCreateSchemaAddsForeignKeysAfterTables(t *testing.T) {
	t.Parallel()

	model := schema.Schema{Tables: []schema.Table{
		{
			Name: "accounts",
			Columns: []schema.Column{
				{Name: "tenant_id", Type: "uuid"},
				{Name: "id", Type: "uuid"},
			},
			PrimaryKey: &schema.PrimaryKey{Name: "pk_accounts", Columns: []string{"tenant_id", "id"}},
		},
		{
			Name: "orders",
			Columns: []schema.Column{
				{Name: "tenant_id", Type: "uuid"},
				{Name: "account_id", Type: "uuid"},
			},
			ForeignKeys: []schema.ForeignKey{{
				Name:              "fk_orders_account",
				Columns:           []string{"tenant_id", "account_id"},
				ReferencedTable:   "accounts",
				ReferencedColumns: []string{"tenant_id", "id"},
				OnDelete:          schema.Cascade,
				OnUpdate:          schema.Restrict,
				Deferrable:        true,
				InitiallyDeferred: true,
			}},
		},
	}}

	sql, err := RenderCreateSchema(model)
	if err != nil {
		t.Fatal(err)
	}
	accounts := strings.Index(sql, `CREATE TABLE "accounts"`)
	orders := strings.Index(sql, `CREATE TABLE "orders"`)
	foreignKey := strings.Index(sql, `ALTER TABLE "orders" ADD CONSTRAINT "fk_orders_account" FOREIGN KEY ("tenant_id", "account_id") REFERENCES "accounts" ("tenant_id", "id") ON DELETE CASCADE ON UPDATE RESTRICT DEFERRABLE INITIALLY DEFERRED;`)
	if accounts < 0 || orders < 0 || foreignKey < 0 || foreignKey < accounts || foreignKey < orders {
		t.Fatalf("unexpected foreign-key SQL ordering:\n%s", sql)
	}
}

func TestRenderMigrationOrdersForeignKeysOutsideDependencies(t *testing.T) {
	t.Parallel()

	foreignKey := schema.ForeignKey{
		Name:              "fk_children_parent",
		Columns:           []string{"parent_id"},
		ReferencedTable:   "parents",
		ReferencedColumns: []string{"id"},
		OnDelete:          schema.Cascade,
	}
	oldType := schema.Column{Name: "parent_id", Type: "integer"}
	newType := schema.Column{Name: "parent_id", Type: "bigint"}

	result, err := RenderMigration(migration.Plan{Operations: []migration.Operation{
		{Kind: migration.AlterColumn, Table: "children", BeforeColumn: &oldType, AfterColumn: &newType, Risk: migration.RiskReview},
		{Kind: migration.AddForeignKey, Table: "children", AfterForeignKey: &foreignKey, Risk: migration.RiskReview},
		{Kind: migration.DropForeignKey, Table: "children", BeforeForeignKey: &foreignKey, Risk: migration.RiskReview},
	}})
	if err != nil {
		t.Fatal(err)
	}

	upDrop := strings.Index(result.Up, `DROP CONSTRAINT "fk_children_parent"`)
	upAlter := strings.Index(result.Up, `ALTER COLUMN "parent_id" TYPE bigint`)
	upAdd := strings.LastIndex(result.Up, `ADD CONSTRAINT "fk_children_parent"`)
	if upDrop < 0 || upAlter < 0 || upAdd < 0 || !(upDrop < upAlter && upAlter < upAdd) {
		t.Fatalf("unexpected up ordering:\n%s", result.Up)
	}

	downDrop := strings.Index(result.Down, `DROP CONSTRAINT "fk_children_parent"`)
	downAlter := strings.Index(result.Down, `ALTER COLUMN "parent_id" TYPE integer`)
	downAdd := strings.LastIndex(result.Down, `ADD CONSTRAINT "fk_children_parent"`)
	if downDrop < 0 || downAlter < 0 || downAdd < 0 || !(downDrop < downAlter && downAlter < downAdd) {
		t.Fatalf("unexpected down ordering:\n%s", result.Down)
	}
}
