package migration

import (
	"testing"

	"github.com/sevlumen/orm/schema"
)

func TestDiffOrdersDependencyChangesAroundColumns(t *testing.T) {
	t.Parallel()
	before := mustSnapshot(t, schema.Schema{Tables: []schema.Table{{
		Name: "users",
		Columns: []schema.Column{
			{Name: "id", Type: "uuid", PrimaryKey: true},
			{Name: "email", Type: "text"},
		},
		Indexes: []schema.Index{{Name: "ix_users_email", Columns: []string{"email"}}},
	}}})
	after := mustSnapshot(t, schema.Schema{Tables: []schema.Table{{
		Name:    "users",
		Columns: []schema.Column{{Name: "id", Type: "uuid", PrimaryKey: true}},
	}}})
	plan, err := Diff(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 2 || plan.Operations[0].Kind != DropIndex || plan.Operations[1].Kind != DropColumn {
		t.Fatalf("unexpected dependency order: %#v", plan.Operations)
	}
}

func TestDiffCreatesAndReplacesNamedMetadata(t *testing.T) {
	t.Parallel()
	before := mustSnapshot(t, schema.Schema{Tables: []schema.Table{{
		Name: "users",
		Columns: []schema.Column{
			{Name: "id", Type: "uuid", PrimaryKey: true},
			{Name: "email", Type: "text"},
		},
		Indexes: []schema.Index{{Name: "ix_users_email", Columns: []string{"email"}}},
	}}})
	after := mustSnapshot(t, schema.Schema{Tables: []schema.Table{{
		Name: "users",
		Columns: []schema.Column{
			{Name: "id", Type: "uuid", PrimaryKey: true},
			{Name: "email", Type: "text"},
		},
		UniqueConstraints: []schema.UniqueConstraint{{Name: "uq_users_email", Columns: []string{"email"}}},
		Checks:            []schema.CheckConstraint{{Name: "ck_users_email", Expression: "length(email) > 3"}},
		Indexes:           []schema.Index{{Name: "ix_users_email", Expression: "lower(email)", Unique: true}},
	}}})
	plan, err := Diff(before, after)
	if err != nil {
		t.Fatal(err)
	}
	want := []OperationKind{DropIndex, CreateIndex, AddUniqueConstraint, AddCheckConstraint}
	if len(plan.Operations) != len(want) {
		t.Fatalf("operations = %#v", plan.Operations)
	}
	for i, kind := range want {
		if plan.Operations[i].Kind != kind {
			t.Fatalf("operation kinds = %#v, want %v", plan.Operations, want)
		}
	}
	if plan.MaxRisk() != RiskReview {
		t.Fatalf("risk = %s, want review", plan.MaxRisk())
	}
}

func TestDiffRejectsPrimaryKeyChange(t *testing.T) {
	t.Parallel()
	before := mustSnapshot(t, schema.Schema{Tables: []schema.Table{{
		Name:       "memberships",
		Columns:    []schema.Column{{Name: "tenant_id", Type: "uuid"}, {Name: "user_id", Type: "uuid"}},
		PrimaryKey: &schema.PrimaryKey{Name: "pk_memberships", Columns: []string{"tenant_id", "user_id"}},
	}}})
	after := mustSnapshot(t, schema.Schema{Tables: []schema.Table{{
		Name:       "memberships",
		Columns:    []schema.Column{{Name: "tenant_id", Type: "uuid"}, {Name: "user_id", Type: "uuid"}},
		PrimaryKey: &schema.PrimaryKey{Name: "pk_memberships", Columns: []string{"user_id", "tenant_id"}},
	}}})
	if _, err := Diff(before, after); err == nil {
		t.Fatal("expected explicit primary-key migration error")
	}
}
