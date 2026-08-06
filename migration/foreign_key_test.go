package migration

import (
	"testing"

	"github.com/sevlumen/orm/schema"
)

func TestDiffCreatesTablesBeforeCyclicForeignKeys(t *testing.T) {
	t.Parallel()

	after := mustSnapshot(t, cyclicForeignKeySchema())
	plan, err := Diff(EmptySnapshot(), after)
	if err != nil {
		t.Fatal(err)
	}
	want := []OperationKind{CreateTable, CreateTable, AddForeignKey, AddForeignKey}
	if len(plan.Operations) != len(want) {
		t.Fatalf("operations = %#v", plan.Operations)
	}
	for i, kind := range want {
		if plan.Operations[i].Kind != kind {
			t.Fatalf("operation %d = %s, want %s", i, plan.Operations[i].Kind, kind)
		}
	}
	if plan.MaxRisk() != RiskReview {
		t.Fatalf("risk = %s, want review", plan.MaxRisk())
	}
}

func TestDiffDropsIncomingForeignKeyBeforeReferencedTable(t *testing.T) {
	t.Parallel()

	before := mustSnapshot(t, parentChildSchema("integer"))
	after := mustSnapshot(t, schema.Schema{Tables: []schema.Table{{
		Name:    "children",
		Columns: []schema.Column{{Name: "id", Type: "integer", PrimaryKey: true}, {Name: "parent_id", Type: "integer"}},
	}}})
	plan, err := Diff(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 2 || plan.Operations[0].Kind != DropForeignKey || plan.Operations[1].Kind != DropTable {
		t.Fatalf("unexpected operations: %#v", plan.Operations)
	}
}

func TestDiffRecreatesForeignKeyAroundColumnTypeChanges(t *testing.T) {
	t.Parallel()

	before := mustSnapshot(t, parentChildSchema("integer"))
	after := mustSnapshot(t, parentChildSchema("bigint"))
	plan, err := Diff(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 4 {
		t.Fatalf("operations = %#v", plan.Operations)
	}
	if plan.Operations[0].Kind != DropForeignKey || plan.Operations[len(plan.Operations)-1].Kind != AddForeignKey {
		t.Fatalf("foreign key is not wrapped around type changes: %#v", plan.Operations)
	}
	for _, operation := range plan.Operations[1 : len(plan.Operations)-1] {
		if operation.Kind != AlterColumn {
			t.Fatalf("middle operation = %s, want alter_column", operation.Kind)
		}
	}
}

func TestSnapshotCanonicalizesForeignKeys(t *testing.T) {
	t.Parallel()

	model := parentChildSchema("integer")
	model.Tables[1].ForeignKeys = append(model.Tables[1].ForeignKeys,
		schema.ForeignKey{Name: "fk_children_backup", Columns: []string{"parent_id"}, ReferencedTable: "parents", ReferencedColumns: []string{"id"}},
	)
	model.Tables[1].ForeignKeys[0], model.Tables[1].ForeignKeys[1] = model.Tables[1].ForeignKeys[1], model.Tables[1].ForeignKeys[0]
	snapshot, err := NewSnapshot(model)
	if err != nil {
		t.Fatal(err)
	}
	foreignKeys := snapshot.Schema.Tables[0].ForeignKeys
	if snapshot.Schema.Tables[0].Name != "children" || len(foreignKeys) != 2 || foreignKeys[0].Name != "fk_children_backup" {
		t.Fatalf("snapshot is not canonical: %#v", snapshot.Schema.Tables)
	}
}

func parentChildSchema(columnType string) schema.Schema {
	return schema.Schema{Tables: []schema.Table{
		{
			Name:    "parents",
			Columns: []schema.Column{{Name: "id", Type: columnType, PrimaryKey: true}},
		},
		{
			Name: "children",
			Columns: []schema.Column{
				{Name: "id", Type: "integer", PrimaryKey: true},
				{Name: "parent_id", Type: columnType},
			},
			ForeignKeys: []schema.ForeignKey{{
				Name:              "fk_children_parent",
				Columns:           []string{"parent_id"},
				ReferencedTable:   "parents",
				ReferencedColumns: []string{"id"},
				OnDelete:          schema.Cascade,
			}},
		},
	}}
}

func cyclicForeignKeySchema() schema.Schema {
	return schema.Schema{Tables: []schema.Table{
		{
			Name: "left_nodes",
			Columns: []schema.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true},
				{Name: "right_id", Type: "uuid", Nullable: true},
			},
			ForeignKeys: []schema.ForeignKey{{Name: "fk_left_right", Columns: []string{"right_id"}, ReferencedTable: "right_nodes", ReferencedColumns: []string{"id"}}},
		},
		{
			Name: "right_nodes",
			Columns: []schema.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true},
				{Name: "left_id", Type: "uuid", Nullable: true},
			},
			ForeignKeys: []schema.ForeignKey{{Name: "fk_right_left", Columns: []string{"left_id"}, ReferencedTable: "left_nodes", ReferencedColumns: []string{"id"}}},
		},
	}}
}
