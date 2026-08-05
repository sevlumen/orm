package schema

import "testing"

func TestValidateRejectsMultipleInlinePrimaryKeys(t *testing.T) {
	t.Parallel()

	model := Schema{Tables: []Table{{
		Name: "memberships",
		Columns: []Column{
			{Name: "user_id", Type: "uuid", PrimaryKey: true},
			{Name: "group_id", Type: "uuid", PrimaryKey: true},
		},
	}}}
	if err := model.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateRejectsEmptySchema(t *testing.T) {
	t.Parallel()
	if err := (Schema{}).Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}
