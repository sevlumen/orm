package migration

import (
	"testing"

	"github.com/sevlumen/orm/schema"
)

func TestSnapshotCanonicalizesNamedMetadata(t *testing.T) {
	t.Parallel()
	value, err := NewSnapshot(schema.Schema{Tables: []schema.Table{{
		Name:    "users",
		Columns: []schema.Column{{Name: "id", Type: "uuid"}, {Name: "email", Type: "text"}},
		UniqueConstraints: []schema.UniqueConstraint{
			{Name: "z_unique", Columns: []string{"email"}},
			{Name: "a_unique", Columns: []string{"id"}},
		},
		Checks:  []schema.CheckConstraint{{Name: "z_check", Expression: "true"}, {Name: "a_check", Expression: "true"}},
		Indexes: []schema.Index{{Name: "z_index", Columns: []string{"email"}}, {Name: "a_index", Columns: []string{"id"}}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	table := value.Schema.Tables[0]
	if table.UniqueConstraints[0].Name != "a_unique" || table.Checks[0].Name != "a_check" || table.Indexes[0].Name != "a_index" {
		t.Fatalf("metadata was not canonicalized: %#v", table)
	}
}
