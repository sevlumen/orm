package migration

import (
	"strings"
	"testing"

	"github.com/sevlumen/orm/schema"
)

func TestSnapshotRoundTrip(t *testing.T) {
	t.Parallel()

	snapshot, err := NewSnapshot(schema.Schema{Tables: []schema.Table{{
		Name:    "users",
		Columns: []schema.Column{{Name: "id", Type: "uuid", PrimaryKey: true}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	data, err := snapshot.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := ParseSnapshot(data)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Version != SnapshotVersion || decoded.Schema.Tables[0].Name != "users" {
		t.Fatalf("unexpected decoded snapshot: %#v", decoded)
	}
}

func TestParseSnapshotRejectsUnknownFields(t *testing.T) {
	t.Parallel()
	_, err := ParseSnapshot([]byte(`{"version":1,"schema":{"tables":[]},"unknown":true}`))
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("expected unknown field error, got %v", err)
	}
}
