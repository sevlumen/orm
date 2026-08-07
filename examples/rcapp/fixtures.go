package rcapp

import (
	orm "github.com/sevlumen/orm"
	"github.com/sevlumen/orm/migration"
	"github.com/sevlumen/orm/schema"
)

// InitialSnapshot is the pre-v1 schema used to exercise an in-place upgrade.
func InitialSnapshot() (migration.Snapshot, error) {
	return migration.NewSnapshot(schema.Schema{Tables: []schema.Table{
		{
			Name: "users",
			Columns: []schema.Column{
				{Name: "id", Type: "bigint", PrimaryKey: true},
				{Name: "email", Type: "text", Unique: true},
				{Name: "active", Type: "boolean", Default: "true"},
			},
		},
		{
			Name: "orders",
			Columns: []schema.Column{
				{Name: "id", Type: "bigint", PrimaryKey: true},
				{Name: "user_id", Type: "bigint"},
				{Name: "total", Type: "bigint"},
			},
			ForeignKeys: []schema.ForeignKey{{
				Name:              "fk_orders_users",
				Columns:           []string{"user_id"},
				ReferencedTable:   "users",
				ReferencedColumns: []string{"id"},
				OnDelete:          schema.Cascade,
			}},
		},
	}})
}

// SafeSnapshot adds only a nullable column and therefore exercises the safe
// migration gate on an already populated legacy database.
func SafeSnapshot() (migration.Snapshot, error) {
	initial, err := InitialSnapshot()
	if err != nil {
		return migration.Snapshot{}, err
	}
	model := initial.Schema
	for tableIndex := range model.Tables {
		if model.Tables[tableIndex].Name == "users" {
			model.Tables[tableIndex].Columns = append(model.Tables[tableIndex].Columns, schema.Column{
				Name:     "legacy_note",
				Type:     "text",
				Nullable: true,
			})
		}
	}
	return migration.NewSnapshot(model)
}

// FinalSnapshot is generated from the maintained final application entities.
func FinalSnapshot() (migration.Snapshot, error) {
	return orm.BuildSnapshot(Account{}, Order{})
}

// DestructiveSnapshot removes legacy_note to exercise destructive-risk gates
// without applying the data-losing migration.
func DestructiveSnapshot() (migration.Snapshot, error) {
	final, err := FinalSnapshot()
	if err != nil {
		return migration.Snapshot{}, err
	}
	model := final.Schema
	for tableIndex := range model.Tables {
		if model.Tables[tableIndex].Name != "accounts" {
			continue
		}
		columns := model.Tables[tableIndex].Columns[:0]
		for _, column := range model.Tables[tableIndex].Columns {
			if column.Name != "legacy_note" {
				columns = append(columns, column)
			}
		}
		model.Tables[tableIndex].Columns = columns
	}
	return migration.NewSnapshot(model)
}

// UpgradeRenames preserves tables, columns, constraints, and data while moving
// the legacy schema to the final application names.
func UpgradeRenames() migration.DiffOptions {
	return migration.DiffOptions{Renames: []migration.Rename{
		{Kind: migration.RenameTable, From: "users", To: "accounts"},
		{Kind: migration.RenameColumn, Table: "accounts", From: "email", To: "login_email"},
		{Kind: migration.RenameColumn, Table: "orders", From: "user_id", To: "account_id"},
		{Kind: migration.RenameConstraint, Table: "orders", From: "fk_orders_users", To: "fk_orders_accounts"},
	}}
}
