package schema

import "testing"

func TestValidateAllowsCompositeInlinePrimaryKey(t *testing.T) {
	t.Parallel()

	model := Schema{Tables: []Table{{
		Name: "memberships",
		Columns: []Column{
			{Name: "user_id", Type: "uuid", PrimaryKey: true},
			{Name: "group_id", Type: "uuid", PrimaryKey: true},
		},
	}}}
	if err := model.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateRejectsMixedInlineAndTablePrimaryKeys(t *testing.T) {
	t.Parallel()

	model := Schema{Tables: []Table{{
		Name: "memberships",
		Columns: []Column{
			{Name: "user_id", Type: "uuid", PrimaryKey: true},
			{Name: "group_id", Type: "uuid"},
		},
		PrimaryKey: &PrimaryKey{
			Name:    "pk_memberships",
			Columns: []string{"user_id", "group_id"},
		},
	}}}
	if err := model.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateAllowsEmptyDatabaseSchema(t *testing.T) {
	t.Parallel()
	if err := (Schema{}).Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}
