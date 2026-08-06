package migration

import (
	"testing"

	"github.com/sevlumen/orm/schema"
)

func TestDiffOrdersNativeObjectsAroundTables(t *testing.T) {
	t.Parallel()

	after := mustSnapshot(t, schema.Schema{
		Extensions: []schema.Extension{{Name: "pgcrypto"}},
		Enums:      []schema.EnumType{{Name: "order_status", Values: []string{"new", "paid"}}},
		Tables: []schema.Table{{
			Name: "orders",
			Columns: []schema.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true},
				{Name: "status", Type: "order_status"},
			},
		}},
	})
	plan, err := Diff(EmptySnapshot(), after)
	if err != nil {
		t.Fatal(err)
	}
	want := []OperationKind{CreateExtension, CreateEnum, CreateTable}
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

func TestDiffRejectsUnsafeNativeObjectChanges(t *testing.T) {
	t.Parallel()

	base := schema.Schema{
		Extensions: []schema.Extension{{Name: "pgcrypto"}},
		Enums:      []schema.EnumType{{Name: "order_status", Values: []string{"new", "paid"}}},
		Tables: []schema.Table{{
			Name:    "orders",
			Columns: []schema.Column{{Name: "status", Type: "order_status"}},
		}},
	}
	before := mustSnapshot(t, base)

	changedEnum := cloneSchema(base)
	changedEnum.Enums[0].Values = append(changedEnum.Enums[0].Values, "cancelled")
	if _, err := Diff(before, mustSnapshot(t, changedEnum)); err == nil {
		t.Fatal("expected enum value change error")
	}

	removedExtension := cloneSchema(base)
	removedExtension.Extensions = nil
	if _, err := Diff(before, mustSnapshot(t, removedExtension)); err == nil {
		t.Fatal("expected extension removal error")
	}

	removedEnumStillUsed := cloneSchema(base)
	removedEnumStillUsed.Enums = nil
	if _, err := Diff(before, mustSnapshot(t, removedEnumStillUsed)); err == nil {
		t.Fatal("expected enum-in-use removal error")
	}
}

func TestDiffDropsEnumAfterDependentTable(t *testing.T) {
	t.Parallel()

	before := mustSnapshot(t, schema.Schema{
		Enums: []schema.EnumType{{Name: "order_status", Values: []string{"new", "paid"}}},
		Tables: []schema.Table{{
			Name:    "orders",
			Columns: []schema.Column{{Name: "status", Type: "order_status"}},
		}},
	})
	after := EmptySnapshot()
	plan, err := Diff(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 2 || plan.Operations[0].Kind != DropTable || plan.Operations[1].Kind != DropEnum {
		t.Fatalf("unexpected operations: %#v", plan.Operations)
	}
	if plan.MaxRisk() != RiskDestructive {
		t.Fatalf("risk = %s, want destructive", plan.MaxRisk())
	}
}

func TestDiffClassifiesGeneratedColumnsAndRejectsExpressionChanges(t *testing.T) {
	t.Parallel()

	before := mustSnapshot(t, schema.Schema{Tables: []schema.Table{{
		Name: "orders",
		Columns: []schema.Column{
			{Name: "subtotal", Type: "integer"},
			{Name: "tax", Type: "integer"},
		},
	}}})
	after := mustSnapshot(t, schema.Schema{Tables: []schema.Table{{
		Name: "orders",
		Columns: []schema.Column{
			{Name: "subtotal", Type: "integer"},
			{Name: "tax", Type: "integer"},
			{Name: "total", Type: "integer", Generated: "subtotal + tax"},
		},
	}}})
	plan, err := Diff(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 1 || plan.Operations[0].Kind != AddColumn || plan.Operations[0].Risk != RiskReview {
		t.Fatalf("unexpected generated-column plan: %#v", plan.Operations)
	}

	changed := cloneSchema(after.Schema)
	changed.Tables[0].Columns[2].Generated = "subtotal + tax + 1"
	if _, err := Diff(after, mustSnapshot(t, changed)); err == nil {
		t.Fatal("expected generated-expression change error")
	}
}

func TestSnapshotCanonicalizesNativeObjects(t *testing.T) {
	t.Parallel()

	snapshot, err := NewSnapshot(schema.Schema{
		Extensions: []schema.Extension{{Name: "uuid-ossp"}, {Name: "pgcrypto"}},
		Enums: []schema.EnumType{
			{Name: "z_status", Values: []string{"z"}},
			{Name: "a_status", Values: []string{"a"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Schema.Extensions[0].Name != "pgcrypto" || snapshot.Schema.Enums[0].Name != "a_status" {
		t.Fatalf("native objects are not canonical: %#v", snapshot.Schema)
	}
}
