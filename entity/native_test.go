package entity

import (
	"encoding/json"
	"testing"
	"time"
)

type nativeEntity struct {
	Payload     json.RawMessage
	Metadata    map[string]any
	Tags        []string
	Scores      []int64
	Moments     []time.Time
	Blobs       [][]byte
	Raw         []byte
	FirstName   string
	LastName    string
	DisplayName string `orm:"generated:first_name || ' ' || last_name"`
}

func TestInferNativePostgreSQLTypes(t *testing.T) {
	t.Parallel()

	model, err := Parse(nativeEntity{})
	if err != nil {
		t.Fatal(err)
	}
	columns := map[string]string{}
	generated := ""
	for _, column := range model.Tables[0].Columns {
		columns[column.Name] = column.Type
		if column.Name == "display_name" {
			generated = column.Generated
		}
	}
	want := map[string]string{
		"payload":  "jsonb",
		"metadata": "jsonb",
		"tags":     "text[]",
		"scores":   "bigint[]",
		"moments":  "timestamptz[]",
		"blobs":    "bytea[]",
		"raw":      "bytea",
	}
	for name, expected := range want {
		if columns[name] != expected {
			t.Fatalf("column %s type = %q, want %q", name, columns[name], expected)
		}
	}
	if generated != "first_name || ' ' || last_name" {
		t.Fatalf("generated expression = %q", generated)
	}
}

func TestParseWithSchemaBuildsExtensionsAndEnums(t *testing.T) {
	t.Parallel()

	model, err := ParseWithSchema(func(schema *SchemaBuilder) {
		schema.Extension("pgcrypto")
		schema.Enum("order_status", "new", "paid")
	}, nativeEntity{})
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Extensions) != 1 || model.Extensions[0].Name != "pgcrypto" {
		t.Fatalf("extensions = %#v", model.Extensions)
	}
	if len(model.Enums) != 1 || model.Enums[0].Name != "order_status" || len(model.Enums[0].Values) != 2 {
		t.Fatalf("enums = %#v", model.Enums)
	}
}

type invalidGeneratedEntity struct {
	Total int `orm:"default:0;generated:1 + 1"`
}

func TestParseRejectsGeneratedDefaultCombination(t *testing.T) {
	t.Parallel()
	if _, err := Parse(invalidGeneratedEntity{}); err == nil {
		t.Fatal("expected generated/default error")
	}
}

type invalidJSONMapEntity struct {
	Value map[int]string
}

func TestParseRejectsNonStringJSONMapKeys(t *testing.T) {
	t.Parallel()
	if _, err := Parse(invalidJSONMapEntity{}); err == nil {
		t.Fatal("expected unsupported JSON map key error")
	}
}
