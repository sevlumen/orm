package migration

import (
	"testing"

	"github.com/sevlumen/orm/schema"
)

func TestDiffWithOptionsPlansOnlyExplicitRenames(t *testing.T) {
	t.Parallel()

	before := mustSnapshot(t, renameBeforeSchema())
	after := mustSnapshot(t, renameAfterSchema())
	options := renameOptions()
	plan, err := DiffWithOptions(before, after, options)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != len(options.Renames) {
		t.Fatalf("operations = %#v", plan.Operations)
	}
	for index, operation := range plan.Operations {
		if operation.Kind != RenameObject || operation.Rename == nil {
			t.Fatalf("operation %d = %#v", index, operation)
		}
		if *operation.Rename != options.Renames[index] {
			t.Fatalf("operation %d rename = %#v, want %#v", index, operation.Rename, options.Renames[index])
		}
	}
	if plan.MaxRisk() != RiskReview {
		t.Fatalf("risk = %s, want review", plan.MaxRisk())
	}
}

func TestDiffWithoutOptionsDoesNotGuessRenames(t *testing.T) {
	t.Parallel()

	plan, err := Diff(mustSnapshot(t, renameBeforeSchema()), mustSnapshot(t, renameAfterSchema()))
	if err == nil {
		for _, operation := range plan.Operations {
			if operation.Kind == RenameObject {
				t.Fatal("ordinary Diff guessed a rename")
			}
		}
		if plan.MaxRisk() != RiskDestructive {
			t.Fatalf("ordinary rename diff risk = %s, want destructive", plan.MaxRisk())
		}
		return
	}
	// A conservative explicit-migration error is also acceptable for complex key changes.
}

func TestDiffWithOptionsRejectsInvalidRenameIntent(t *testing.T) {
	t.Parallel()

	before := mustSnapshot(t, renameBeforeSchema())
	after := mustSnapshot(t, renameAfterSchema())
	tests := []Rename{
		{Kind: RenameTable, From: "missing", To: "customers"},
		{Kind: RenameTable, From: "accounts", To: "orders"},
		{Kind: RenameColumn, Table: "accounts", From: "missing", To: "customer_id"},
		{Kind: RenameIndex, Table: "accounts", From: "missing", To: "ix_customers_email"},
		{Kind: RenameConstraint, Table: "accounts", From: "missing", To: "pk_customers"},
		{Kind: RenameEnum, From: "missing", To: "order_status"},
		{Kind: RenameTable, From: "accounts", To: "accounts"},
	}
	for _, intent := range tests {
		intent := intent
		t.Run(string(intent.Kind)+"_"+intent.From, func(t *testing.T) {
			t.Parallel()
			if _, err := DiffWithOptions(before, after, DiffOptions{Renames: []Rename{intent}}); err == nil {
				t.Fatalf("expected rename error for %#v", intent)
			}
		})
	}
}

func renameOptions() DiffOptions {
	return DiffOptions{Renames: []Rename{
		{Kind: RenameEnum, From: "old_status", To: "order_status"},
		{Kind: RenameTable, From: "accounts", To: "customers"},
		{Kind: RenameColumn, Table: "customers", From: "id", To: "customer_id"},
		{Kind: RenameIndex, Table: "customers", From: "ix_accounts_email", To: "ix_customers_email"},
		{Kind: RenameConstraint, Table: "customers", From: "pk_accounts", To: "pk_customers"},
		{Kind: RenameColumn, Table: "orders", From: "account_id", To: "customer_id"},
		{Kind: RenameConstraint, Table: "orders", From: "fk_orders_account", To: "fk_orders_customer"},
	}}
}

func renameBeforeSchema() schema.Schema {
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
				ForeignKeys: []schema.ForeignKey{{
					Name:              "fk_orders_account",
					Columns:           []string{"account_id"},
					ReferencedTable:   "accounts",
					ReferencedColumns: []string{"id"},
				}},
			},
		},
	}
}

func renameAfterSchema() schema.Schema {
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
				ForeignKeys: []schema.ForeignKey{{
					Name:              "fk_orders_customer",
					Columns:           []string{"customer_id"},
					ReferencedTable:   "customers",
					ReferencedColumns: []string{"customer_id"},
				}},
			},
		},
	}
}
