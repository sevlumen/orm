package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sevlumen/orm/migration"
	"github.com/sevlumen/orm/schema"
)

func TestExplicitRenameMigrationAgainstPostgreSQL(t *testing.T) {
	database := openIntegrationDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	suffix := fmt.Sprintf("%x", time.Now().UnixNano())
	oldEnum := "sl_old_status_" + suffix
	newEnum := "sl_order_status_" + suffix
	oldAccounts := "sl_accounts_" + suffix
	newCustomers := "sl_customers_" + suffix
	orders := "sl_orders_" + suffix
	oldIndex := "ix_accounts_email_" + suffix
	newIndex := "ix_customers_email_" + suffix
	oldPrimary := "pk_accounts_" + suffix
	newPrimary := "pk_customers_" + suffix
	oldForeignKey := "fk_orders_account_" + suffix
	newForeignKey := "fk_orders_customer_" + suffix

	beforeModel := renameIntegrationBefore(oldEnum, oldAccounts, orders, oldIndex, oldPrimary, oldForeignKey)
	afterModel := renameIntegrationAfter(newEnum, newCustomers, orders, newIndex, newPrimary, newForeignKey)
	before := mustRenameSnapshot(t, beforeModel)
	after := mustRenameSnapshot(t, afterModel)
	options := migration.DiffOptions{Renames: []migration.Rename{
		{Kind: migration.RenameEnum, From: oldEnum, To: newEnum},
		{Kind: migration.RenameTable, From: oldAccounts, To: newCustomers},
		{Kind: migration.RenameColumn, Table: newCustomers, From: "id", To: "customer_id"},
		{Kind: migration.RenameIndex, Table: newCustomers, From: oldIndex, To: newIndex},
		{Kind: migration.RenameConstraint, Table: newCustomers, From: oldPrimary, To: newPrimary},
		{Kind: migration.RenameColumn, Table: orders, From: "account_id", To: "customer_id"},
		{Kind: migration.RenameConstraint, Table: orders, From: oldForeignKey, To: newForeignKey},
	}}

	initialSQL, err := RenderCreateSchema(beforeModel)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := migration.DiffWithOptions(before, after, options)
	if err != nil {
		t.Fatal(err)
	}
	generated, err := RenderMigration(plan)
	if err != nil {
		t.Fatal(err)
	}

	oldQualified := quoteIdentifier(oldAccounts)
	newQualified := quoteIdentifier(newCustomers)
	ordersQualified := quoteIdentifier(orders)
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		for _, table := range []string{orders, oldAccounts, newCustomers} {
			_, _ = database.ExecContext(cleanupCtx, "DROP TABLE IF EXISTS "+quoteIdentifier(table)+" CASCADE")
		}
		for _, enum := range []string{oldEnum, newEnum} {
			_, _ = database.ExecContext(cleanupCtx, "DROP TYPE IF EXISTS "+quoteIdentifier(enum)+" CASCADE")
		}
	}()

	executeIntegrationScript(t, ctx, database, initialSQL)
	if _, err := database.ExecContext(ctx, "INSERT INTO "+oldQualified+" (id, email) VALUES (42, 'customer@example.com')"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, "INSERT INTO "+ordersQualified+" (id, account_id, status) VALUES (7, 42, 'new')"); err != nil {
		t.Fatal(err)
	}

	executeIntegrationScript(t, ctx, database, generated.Up)
	var customerID, orderCustomerID int64
	var email, status string
	if err := database.QueryRowContext(ctx, "SELECT customer_id, email FROM "+newQualified).Scan(&customerID, &email); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, "SELECT customer_id, status::text FROM "+ordersQualified).Scan(&orderCustomerID, &status); err != nil {
		t.Fatal(err)
	}
	if customerID != 42 || orderCustomerID != 42 || email != "customer@example.com" || status != "new" {
		t.Fatalf("rename changed data: customer=%d order=%d email=%q status=%q", customerID, orderCustomerID, email, status)
	}
	if _, err := database.ExecContext(ctx, "INSERT INTO "+ordersQualified+" (id, customer_id, status) VALUES (8, 999, 'new')"); err == nil {
		t.Fatal("expected renamed foreign key to remain enforced")
	}
	var indexExists, constraintsExist bool
	if err := database.QueryRowContext(ctx, "SELECT to_regclass($1) IS NOT NULL", newIndex).Scan(&indexExists); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `SELECT count(*) = 2 FROM pg_constraint WHERE conname IN ($1, $2)`, newPrimary, newForeignKey).Scan(&constraintsExist); err != nil {
		t.Fatal(err)
	}
	if !indexExists || !constraintsExist {
		t.Fatalf("renamed metadata missing: index=%v constraints=%v", indexExists, constraintsExist)
	}

	executeIntegrationScript(t, ctx, database, generated.Down)
	if err := database.QueryRowContext(ctx, "SELECT id, email FROM "+oldQualified).Scan(&customerID, &email); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, "SELECT account_id, status::text FROM "+ordersQualified).Scan(&orderCustomerID, &status); err != nil {
		t.Fatal(err)
	}
	if customerID != 42 || orderCustomerID != 42 || email != "customer@example.com" || status != "new" {
		t.Fatalf("rollback rename changed data: customer=%d order=%d email=%q status=%q", customerID, orderCustomerID, email, status)
	}
}

func renameIntegrationBefore(enumName, accounts, orders, index, primary, foreignKey string) schema.Schema {
	return schema.Schema{
		Enums: []schema.EnumType{{Name: enumName, Values: []string{"new", "paid"}}},
		Tables: []schema.Table{
			{
				Name: accounts,
				Columns: []schema.Column{
					{Name: "id", Type: "bigint"},
					{Name: "email", Type: "text"},
				},
				PrimaryKey: &schema.PrimaryKey{Name: primary, Columns: []string{"id"}},
				Indexes:    []schema.Index{{Name: index, Columns: []string{"email"}}},
			},
			{
				Name: orders,
				Columns: []schema.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true},
					{Name: "account_id", Type: "bigint"},
					{Name: "status", Type: enumName},
				},
				ForeignKeys: []schema.ForeignKey{{Name: foreignKey, Columns: []string{"account_id"}, ReferencedTable: accounts, ReferencedColumns: []string{"id"}}},
			},
		},
	}
}

func renameIntegrationAfter(enumName, customers, orders, index, primary, foreignKey string) schema.Schema {
	return schema.Schema{
		Enums: []schema.EnumType{{Name: enumName, Values: []string{"new", "paid"}}},
		Tables: []schema.Table{
			{
				Name: customers,
				Columns: []schema.Column{
					{Name: "customer_id", Type: "bigint"},
					{Name: "email", Type: "text"},
				},
				PrimaryKey: &schema.PrimaryKey{Name: primary, Columns: []string{"customer_id"}},
				Indexes:    []schema.Index{{Name: index, Columns: []string{"email"}}},
			},
			{
				Name: orders,
				Columns: []schema.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true},
					{Name: "customer_id", Type: "bigint"},
					{Name: "status", Type: enumName},
				},
				ForeignKeys: []schema.ForeignKey{{Name: foreignKey, Columns: []string{"customer_id"}, ReferencedTable: customers, ReferencedColumns: []string{"customer_id"}}},
			},
		},
	}
}
