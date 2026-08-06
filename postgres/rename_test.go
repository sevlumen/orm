package postgres

import (
	"strings"
	"testing"

	"github.com/sevlumen/orm/migration"
	"github.com/sevlumen/orm/schema"
)

func TestRenderExplicitRenamesInDeclaredOrder(t *testing.T) {
	t.Parallel()

	plan, err := migration.DiffWithOptions(
		mustRenameSnapshot(t, renameBeforeModel()),
		mustRenameSnapshot(t, renameAfterModel()),
		renamePlanOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := RenderMigration(plan)
	if err != nil {
		t.Fatal(err)
	}

	wantUp := []string{
		`ALTER TYPE "old_status" RENAME TO "order_status";`,
		`ALTER TABLE "accounts" RENAME TO "customers";`,
		`ALTER TABLE "customers" RENAME COLUMN "id" TO "customer_id";`,
		`ALTER INDEX "ix_accounts_email" RENAME TO "ix_customers_email";`,
		`ALTER TABLE "customers" RENAME CONSTRAINT "pk_accounts" TO "pk_customers";`,
		`ALTER TABLE "orders" RENAME COLUMN "account_id" TO "customer_id";`,
		`ALTER TABLE "orders" RENAME CONSTRAINT "fk_orders_account" TO "fk_orders_customer";`,
	}
	assertOrderedStatements(t, result.Up, wantUp)

	wantDown := []string{
		`ALTER TABLE "orders" RENAME CONSTRAINT "fk_orders_customer" TO "fk_orders_account";`,
		`ALTER TABLE "orders" RENAME COLUMN "customer_id" TO "account_id";`,
		`ALTER TABLE "customers" RENAME CONSTRAINT "pk_customers" TO "pk_accounts";`,
		`ALTER INDEX "ix_customers_email" RENAME TO "ix_accounts_email";`,
		`ALTER TABLE "customers" RENAME COLUMN "customer_id" TO "id";`,
		`ALTER TABLE "customers" RENAME TO "accounts";`,
		`ALTER TYPE "order_status" RENAME TO "old_status";`,
	}
	assertOrderedStatements(t, result.Down, wantDown)
}

func TestRenameFreesSourceNameBeforeCreate(t *testing.T) {
	t.Parallel()

	before := mustRenameSnapshot(t, schema.Schema{Tables: []schema.Table{{
		Name:    "accounts",
		Columns: []schema.Column{{Name: "id", Type: "bigint", PrimaryKey: true}},
	}}})
	after := mustRenameSnapshot(t, schema.Schema{Tables: []schema.Table{
		{Name: "customers", Columns: []schema.Column{{Name: "id", Type: "bigint", PrimaryKey: true}}},
		{Name: "accounts", Columns: []schema.Column{{Name: "id", Type: "bigint", PrimaryKey: true}}},
	}})
	plan, err := migration.DiffWithOptions(before, after, migration.DiffOptions{Renames: []migration.Rename{{Kind: migration.RenameTable, From: "accounts", To: "customers"}}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := RenderMigration(plan)
	if err != nil {
		t.Fatal(err)
	}
	rename := strings.Index(result.Up, `ALTER TABLE "accounts" RENAME TO "customers";`)
	create := strings.Index(result.Up, `CREATE TABLE "accounts"`)
	if rename < 0 || create < 0 || rename > create {
		t.Fatalf("source name is not freed before reuse:\n%s", result.Up)
	}
	drop := strings.Index(result.Down, `DROP TABLE "accounts";`)
	reverseRename := strings.Index(result.Down, `ALTER TABLE "customers" RENAME TO "accounts";`)
	if drop < 0 || reverseRename < 0 || drop > reverseRename {
		t.Fatalf("down SQL does not free source name before reverse rename:\n%s", result.Down)
	}
}

func assertOrderedStatements(t *testing.T, sql string, statements []string) {
	t.Helper()
	previous := -1
	for _, statement := range statements {
		position := strings.Index(sql, statement)
		if position < 0 {
			t.Fatalf("missing statement %q in:\n%s", statement, sql)
		}
		if position <= previous {
			t.Fatalf("statement %q is out of order in:\n%s", statement, sql)
		}
		previous = position
	}
}

func mustRenameSnapshot(t *testing.T, model schema.Schema) migration.Snapshot {
	t.Helper()
	snapshot, err := migration.NewSnapshot(model)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func renamePlanOptions() migration.DiffOptions {
	return migration.DiffOptions{Renames: []migration.Rename{
		{Kind: migration.RenameEnum, From: "old_status", To: "order_status"},
		{Kind: migration.RenameTable, From: "accounts", To: "customers"},
		{Kind: migration.RenameColumn, Table: "customers", From: "id", To: "customer_id"},
		{Kind: migration.RenameIndex, Table: "customers", From: "ix_accounts_email", To: "ix_customers_email"},
		{Kind: migration.RenameConstraint, Table: "customers", From: "pk_accounts", To: "pk_customers"},
		{Kind: migration.RenameColumn, Table: "orders", From: "account_id", To: "customer_id"},
		{Kind: migration.RenameConstraint, Table: "orders", From: "fk_orders_account", To: "fk_orders_customer"},
	}}
}

func renameBeforeModel() schema.Schema {
	return schema.Schema{
		Enums: []schema.EnumType{{Name: "old_status", Values: []string{"new", "paid"}}},
		Tables: []schema.Table{
			{
				Name: "accounts",
				Columns: []schema.Column{
					{Name: "id", Type: "uuid"},
					{Name: "email", Type: "text"},
				},
				PrimaryKey: &schema.PrimaryKey{Name: "pk_accounts", Columns: []string{"id"}},
				Indexes:    []schema.Index{{Name: "ix_accounts_email", Columns: []string{"email"}}},
			},
			{
				Name: "orders",
				Columns: []schema.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "account_id", Type: "uuid"},
					{Name: "status", Type: "old_status"},
				},
				ForeignKeys: []schema.ForeignKey{{Name: "fk_orders_account", Columns: []string{"account_id"}, ReferencedTable: "accounts", ReferencedColumns: []string{"id"}}},
			},
		},
	}
}

func renameAfterModel() schema.Schema {
	return schema.Schema{
		Enums: []schema.EnumType{{Name: "order_status", Values: []string{"new", "paid"}}},
		Tables: []schema.Table{
			{
				Name: "customers",
				Columns: []schema.Column{
					{Name: "customer_id", Type: "uuid"},
					{Name: "email", Type: "text"},
				},
				PrimaryKey: &schema.PrimaryKey{Name: "pk_customers", Columns: []string{"customer_id"}},
				Indexes:    []schema.Index{{Name: "ix_customers_email", Columns: []string{"email"}}},
			},
			{
				Name: "orders",
				Columns: []schema.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "customer_id", Type: "uuid"},
					{Name: "status", Type: "order_status"},
				},
				ForeignKeys: []schema.ForeignKey{{Name: "fk_orders_customer", Columns: []string{"customer_id"}, ReferencedTable: "customers", ReferencedColumns: []string{"customer_id"}}},
			},
		},
	}
}
