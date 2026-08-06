package query

import (
	"reflect"
	"testing"
)

func FuzzSelectValueRemainsParameterized(f *testing.F) {
	f.Add("normal@example.com")
	f.Add(`x' OR TRUE --`)
	f.Add("$1; DROP TABLE users; --")
	f.Add("")

	f.Fuzz(func(t *testing.T, value string) {
		table, err := NewTable[testUser]("users", []string{"email"}, func(RowScanner) (testUser, error) {
			return testUser{}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		email, err := NewColumn[testUser, string](table, "email")
		if err != nil {
			t.Fatal(err)
		}
		statement, err := Select(table).Where(email.Eq(value)).Build()
		if err != nil {
			t.Fatal(err)
		}
		wantSQL := `SELECT "email" FROM "users" WHERE "email" = $1`
		if statement.SQL != wantSQL {
			t.Fatalf("SQL = %q, want %q", statement.SQL, wantSQL)
		}
		if !reflect.DeepEqual(statement.Args, []any{value}) {
			t.Fatalf("Args = %#v", statement.Args)
		}
	})
}

func FuzzMetadataValidationNeverPanics(f *testing.F) {
	f.Add("users", "email")
	f.Add("", "")
	f.Add("users\x00archive", "email")
	f.Add("users", "email\x00raw")

	f.Fuzz(func(t *testing.T, tableName, columnName string) {
		table, err := NewTable[testUser](tableName, []string{columnName}, func(RowScanner) (testUser, error) {
			return testUser{}, nil
		})
		if err != nil {
			return
		}
		if _, err := NewColumn[testUser, string](table, columnName); err != nil {
			t.Fatalf("column accepted by projection but rejected by metadata: %v", err)
		}
	})
}
