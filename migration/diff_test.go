package migration

import (
	"testing"

	"github.com/sevlumen/orm/schema"
)

func mustSnapshot(t *testing.T, model schema.Schema) Snapshot {
	t.Helper()
	snapshot, err := NewSnapshot(model)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestDiffOrdersAndClassifiesOperations(t *testing.T) {
	t.Parallel()

	before := mustSnapshot(t, schema.Schema{Tables: []schema.Table{{
		Name: "users",
		Columns: []schema.Column{
			{Name: "id", Type: "uuid", PrimaryKey: true},
			{Name: "legacy", Type: "text", Nullable: true},
		},
	}}})
	after := mustSnapshot(t, schema.Schema{Tables: []schema.Table{{
		Name: "users",
		Columns: []schema.Column{
			{Name: "id", Type: "uuid", PrimaryKey: true},
			{Name: "email", Type: "text", Nullable: false},
		},
	}}})

	plan, err := Diff(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 2 {
		t.Fatalf("operations = %d, want 2", len(plan.Operations))
	}
	if plan.Operations[0].Kind != AddColumn || plan.Operations[0].Risk != RiskReview {
		t.Fatalf("unexpected first operation: %#v", plan.Operations[0])
	}
	if plan.Operations[1].Kind != DropColumn || plan.MaxRisk() != RiskDestructive {
		t.Fatalf("unexpected plan: %#v", plan)
	}
}

func TestDiffRejectsImplicitUniqueChange(t *testing.T) {
	t.Parallel()
	before := mustSnapshot(t, schema.Schema{Tables: []schema.Table{{Name: "users", Columns: []schema.Column{{Name: "email", Type: "text"}}}}})
	after := mustSnapshot(t, schema.Schema{Tables: []schema.Table{{Name: "users", Columns: []schema.Column{{Name: "email", Type: "text", Unique: true}}}}})
	if _, err := Diff(before, after); err == nil {
		t.Fatal("expected explicit migration error")
	}
}
