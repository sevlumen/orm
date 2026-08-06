package schema

import "testing"

func TestValidateForeignKeys(t *testing.T) {
	t.Parallel()

	model := Schema{Tables: []Table{
		{
			Name: "accounts",
			Columns: []Column{
				{Name: "tenant_id", Type: "uuid"},
				{Name: "id", Type: "uuid"},
			},
			PrimaryKey: &PrimaryKey{Name: "pk_accounts", Columns: []string{"tenant_id", "id"}},
		},
		{
			Name: "orders",
			Columns: []Column{
				{Name: "tenant_id", Type: "uuid"},
				{Name: "account_id", Type: "uuid"},
			},
			ForeignKeys: []ForeignKey{{
				Name:              "fk_orders_account",
				Columns:           []string{"tenant_id", "account_id"},
				ReferencedTable:   "accounts",
				ReferencedColumns: []string{"tenant_id", "id"},
				OnDelete:          Cascade,
				OnUpdate:          Cascade,
				Deferrable:        true,
				InitiallyDeferred: true,
			}},
		},
	}}
	if err := model.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateForeignKeyRejectsInvalidReferences(t *testing.T) {
	t.Parallel()

	tests := map[string]Schema{
		"unknown table": {Tables: []Table{{
			Name:        "orders",
			Columns:     []Column{{Name: "account_id", Type: "uuid"}},
			ForeignKeys: []ForeignKey{{Name: "fk_orders_account", Columns: []string{"account_id"}, ReferencedTable: "accounts", ReferencedColumns: []string{"id"}}},
		}}},
		"unknown local column": {Tables: []Table{
			{Name: "accounts", Columns: []Column{{Name: "id", Type: "uuid", PrimaryKey: true}}},
			{Name: "orders", Columns: []Column{{Name: "account_id", Type: "uuid"}}, ForeignKeys: []ForeignKey{{Name: "fk_orders_account", Columns: []string{"missing"}, ReferencedTable: "accounts", ReferencedColumns: []string{"id"}}}},
		}},
		"arity mismatch": {Tables: []Table{
			{Name: "accounts", Columns: []Column{{Name: "tenant_id", Type: "uuid"}, {Name: "id", Type: "uuid"}}, PrimaryKey: &PrimaryKey{Name: "pk_accounts", Columns: []string{"tenant_id", "id"}}},
			{Name: "orders", Columns: []Column{{Name: "account_id", Type: "uuid"}}, ForeignKeys: []ForeignKey{{Name: "fk_orders_account", Columns: []string{"account_id"}, ReferencedTable: "accounts", ReferencedColumns: []string{"tenant_id", "id"}}}},
		}},
		"unbacked referenced columns": {Tables: []Table{
			{Name: "accounts", Columns: []Column{{Name: "id", Type: "uuid"}, {Name: "email", Type: "text"}}, PrimaryKey: &PrimaryKey{Name: "pk_accounts", Columns: []string{"id"}}},
			{Name: "orders", Columns: []Column{{Name: "account_email", Type: "text"}}, ForeignKeys: []ForeignKey{{Name: "fk_orders_account", Columns: []string{"account_email"}, ReferencedTable: "accounts", ReferencedColumns: []string{"email"}}}},
		}},
		"set null non-nullable": {Tables: []Table{
			{Name: "accounts", Columns: []Column{{Name: "id", Type: "uuid", PrimaryKey: true}}},
			{Name: "orders", Columns: []Column{{Name: "account_id", Type: "uuid"}}, ForeignKeys: []ForeignKey{{Name: "fk_orders_account", Columns: []string{"account_id"}, ReferencedTable: "accounts", ReferencedColumns: []string{"id"}, OnDelete: SetNull}}},
		}},
		"deferred but not deferrable": {Tables: []Table{
			{Name: "accounts", Columns: []Column{{Name: "id", Type: "uuid", PrimaryKey: true}}},
			{Name: "orders", Columns: []Column{{Name: "account_id", Type: "uuid"}}, ForeignKeys: []ForeignKey{{Name: "fk_orders_account", Columns: []string{"account_id"}, ReferencedTable: "accounts", ReferencedColumns: []string{"id"}, InitiallyDeferred: true}}},
		}},
	}

	for name, model := range tests {
		name, model := name, model
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := model.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestValidateAllowsCyclicForeignKeys(t *testing.T) {
	t.Parallel()

	model := Schema{Tables: []Table{
		{
			Name: "left_nodes",
			Columns: []Column{
				{Name: "id", Type: "uuid", PrimaryKey: true},
				{Name: "right_id", Type: "uuid", Nullable: true},
			},
			ForeignKeys: []ForeignKey{{Name: "fk_left_right", Columns: []string{"right_id"}, ReferencedTable: "right_nodes", ReferencedColumns: []string{"id"}}},
		},
		{
			Name: "right_nodes",
			Columns: []Column{
				{Name: "id", Type: "uuid", PrimaryKey: true},
				{Name: "left_id", Type: "uuid", Nullable: true},
			},
			ForeignKeys: []ForeignKey{{Name: "fk_right_left", Columns: []string{"left_id"}, ReferencedTable: "left_nodes", ReferencedColumns: []string{"id"}}},
		},
	}}
	if err := model.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}
