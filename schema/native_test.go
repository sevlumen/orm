package schema

import "testing"

func TestValidateNativeSchemaObjects(t *testing.T) {
	t.Parallel()

	model := Schema{
		Extensions: []Extension{{Name: "pgcrypto"}},
		Enums:      []EnumType{{Name: "order_status", Values: []string{"new", "paid", "cancelled"}}},
		Tables: []Table{{
			Name: "orders",
			Columns: []Column{
				{Name: "id", Type: "uuid", PrimaryKey: true},
				{Name: "status", Type: "order_status"},
				{Name: "subtotal", Type: "numeric"},
				{Name: "tax", Type: "numeric"},
				{Name: "total", Type: "numeric", Generated: "subtotal + tax"},
			},
		}},
	}
	if err := model.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateRejectsInvalidNativeSchemaObjects(t *testing.T) {
	t.Parallel()

	tests := map[string]Schema{
		"duplicate extension": {
			Extensions: []Extension{{Name: "pgcrypto"}, {Name: "pgcrypto"}},
		},
		"duplicate enum": {
			Enums: []EnumType{{Name: "status", Values: []string{"a"}}, {Name: "status", Values: []string{"b"}}},
		},
		"empty enum": {
			Enums: []EnumType{{Name: "status"}},
		},
		"duplicate enum value": {
			Enums: []EnumType{{Name: "status", Values: []string{"a", "a"}}},
		},
		"enum table conflict": {
			Enums:  []EnumType{{Name: "orders", Values: []string{"new"}}},
			Tables: []Table{{Name: "orders", Columns: []Column{{Name: "id", Type: "uuid"}}}},
		},
		"generated with default": {
			Tables: []Table{{Name: "orders", Columns: []Column{{Name: "total", Type: "numeric", Default: "0", Generated: "1 + 1"}}}},
		},
		"generated NUL": {
			Tables: []Table{{Name: "orders", Columns: []Column{{Name: "total", Type: "numeric", Generated: "1\x00+1"}}}},
		},
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
