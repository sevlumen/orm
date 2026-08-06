package migration

import (
	"reflect"
	"testing"
)

func FuzzParseSnapshot(f *testing.F) {
	valid, err := EmptySnapshot().Marshal()
	if err != nil {
		f.Fatal(err)
	}
	for _, seed := range [][]byte{
		valid,
		{},
		[]byte(`null`),
		[]byte(`{"version":1,"schema":{"tables":[],"enums":[],"extensions":[]}}`),
		[]byte(`{"version":1,"schema":{},"unknown":"'; DROP TABLE users; --"}`),
		[]byte(`{"version":1,"schema":{}} {"version":1,"schema":{}}`),
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input []byte) {
		snapshot, err := ParseSnapshot(input)
		if err != nil {
			return
		}
		canonical, err := snapshot.Marshal()
		if err != nil {
			t.Fatalf("accepted snapshot does not marshal: %v", err)
		}
		reparsed, err := ParseSnapshot(canonical)
		if err != nil {
			t.Fatalf("canonical snapshot does not parse: %v", err)
		}
		if !reflect.DeepEqual(snapshot, reparsed) {
			t.Fatalf("snapshot round trip changed value: before=%#v after=%#v", snapshot, reparsed)
		}
	})
}

func FuzzRenameValidate(f *testing.F) {
	for _, seed := range []struct {
		kind, table, from, to string
	}{
		{string(RenameTable), "", "users", "accounts"},
		{string(RenameColumn), "users", "name", "display_name"},
		{string(RenameIndex), "users", "ix_old", "ix_new"},
		{string(RenameConstraint), "users", "pk_old", "pk_new"},
		{string(RenameEnum), "", "status", "account_status"},
		{"'; DROP TABLE users; --", "users", "a", "b"},
	} {
		f.Add(seed.kind, seed.table, seed.from, seed.to)
	}

	f.Fuzz(func(t *testing.T, kind, table, from, to string) {
		rename := Rename{Kind: RenameKind(kind), Table: table, From: from, To: to}
		first := rename.Validate()
		second := rename.Validate()
		if (first == nil) != (second == nil) {
			t.Fatalf("rename validation is non-deterministic: first=%v second=%v", first, second)
		}
	})
}
