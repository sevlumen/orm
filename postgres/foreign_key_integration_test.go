package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sevlumen/orm/schema"
)

func TestGeneratedForeignKeysAgainstPostgreSQL(t *testing.T) {
	database := openIntegrationDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	suffix := fmt.Sprintf("%x", time.Now().UnixNano())
	accounts := "sl_fk_accounts_" + suffix
	orders := "sl_fk_orders_" + suffix
	leftNodes := "sl_fk_left_" + suffix
	rightNodes := "sl_fk_right_" + suffix

	model := schema.Schema{Tables: []schema.Table{
		{
			Name: accounts,
			Columns: []schema.Column{
				{Name: "tenant_id", Type: "bigint"},
				{Name: "id", Type: "bigint"},
			},
			PrimaryKey: &schema.PrimaryKey{Name: "pk_" + accounts, Columns: []string{"tenant_id", "id"}},
		},
		{
			Name: orders,
			Columns: []schema.Column{
				{Name: "id", Type: "bigint", PrimaryKey: true},
				{Name: "tenant_id", Type: "bigint"},
				{Name: "account_id", Type: "bigint"},
			},
			ForeignKeys: []schema.ForeignKey{{
				Name:              "fk_" + orders + "_account",
				Columns:           []string{"tenant_id", "account_id"},
				ReferencedTable:   accounts,
				ReferencedColumns: []string{"tenant_id", "id"},
				OnDelete:          schema.Cascade,
				OnUpdate:          schema.Cascade,
				Deferrable:        true,
				InitiallyDeferred: true,
			}},
		},
		{
			Name: leftNodes,
			Columns: []schema.Column{
				{Name: "id", Type: "bigint", PrimaryKey: true},
				{Name: "right_id", Type: "bigint", Nullable: true},
			},
			ForeignKeys: []schema.ForeignKey{{Name: "fk_" + leftNodes + "_right", Columns: []string{"right_id"}, ReferencedTable: rightNodes, ReferencedColumns: []string{"id"}}},
		},
		{
			Name: rightNodes,
			Columns: []schema.Column{
				{Name: "id", Type: "bigint", PrimaryKey: true},
				{Name: "left_id", Type: "bigint", Nullable: true},
			},
			ForeignKeys: []schema.ForeignKey{{Name: "fk_" + rightNodes + "_left", Columns: []string{"left_id"}, ReferencedTable: leftNodes, ReferencedColumns: []string{"id"}}},
		},
	}}

	ddl, err := RenderCreateSchema(model)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		for _, table := range []string{orders, accounts, leftNodes, rightNodes} {
			if _, cleanupErr := database.ExecContext(cleanupCtx, "DROP TABLE IF EXISTS "+quoteIdentifier(table)+" CASCADE"); cleanupErr != nil {
				t.Logf("cleanup %s failed: %v", table, cleanupErr)
			}
		}
	}()

	executeIntegrationScript(t, ctx, database, ddl)

	qualifiedAccounts := quoteIdentifier(accounts)
	qualifiedOrders := quoteIdentifier(orders)

	if _, err := database.ExecContext(ctx, "INSERT INTO "+qualifiedAccounts+" (tenant_id, id) VALUES (1, 10)"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, "INSERT INTO "+qualifiedOrders+" (id, tenant_id, account_id) VALUES (1, 1, 10)"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, "INSERT INTO "+qualifiedOrders+" (id, tenant_id, account_id) VALUES (2, 1, 999)"); err == nil {
		t.Fatal("expected foreign-key violation")
	}

	if _, err := database.ExecContext(ctx, "DELETE FROM "+qualifiedAccounts+" WHERE tenant_id = 1 AND id = 10"); err != nil {
		t.Fatal(err)
	}
	var orderCount int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM "+qualifiedOrders).Scan(&orderCount); err != nil {
		t.Fatal(err)
	}
	if orderCount != 0 {
		t.Fatalf("cascade left %d orders", orderCount)
	}

	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.ExecContext(ctx, "INSERT INTO "+qualifiedOrders+" (id, tenant_id, account_id) VALUES (3, 2, 20)"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO "+qualifiedAccounts+" (tenant_id, id) VALUES (2, 20)"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	committed = true

	var foreignKeyCount int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM pg_constraint WHERE contype = 'f' AND conrelid = ANY(ARRAY[to_regclass($1), to_regclass($2), to_regclass($3)]::oid[])`, orders, leftNodes, rightNodes).Scan(&foreignKeyCount); err != nil {
		t.Fatal(err)
	}
	if foreignKeyCount != 3 {
		t.Fatalf("foreign key count = %d, want 3", foreignKeyCount)
	}
}
