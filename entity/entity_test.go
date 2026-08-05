package entity

import "testing"

func TestSnakeCase(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"ID":         "id",
		"UserID":     "user_id",
		"HTTPServer": "http_server",
		"CreatedAt":  "created_at",
	}
	for input, want := range tests {
		input, want := input, want
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			if got := snakeCase(input); got != want {
				t.Fatalf("snakeCase(%q) = %q, want %q", input, got, want)
			}
		})
	}
}

func TestExplicitTypeAllowsCustomGoType(t *testing.T) {
	t.Parallel()

	type Metadata map[string]string
	type Event struct {
		Payload Metadata `orm:"type:jsonb"`
	}
	model, err := Parse(Event{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got := model.Tables[0].Columns[0].Type; got != "jsonb" {
		t.Fatalf("column type = %q, want jsonb", got)
	}
}

func TestUnknownTagFails(t *testing.T) {
	t.Parallel()

	type Invalid struct {
		Name string `orm:"unknown"`
	}
	if _, err := Parse(Invalid{}); err == nil {
		t.Fatal("expected unknown tag error")
	}
}

func TestTypedNilPointerEntity(t *testing.T) {
	t.Parallel()

	type User struct{ ID int }
	var user *User
	if _, err := Parse(user); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
}
