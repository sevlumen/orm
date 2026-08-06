package query

import (
	"reflect"
	"strings"
	"testing"
)

func TestTypedValuesAreAlwaysParameters(t *testing.T) {
	table, columns := benchmarkUserMetadata(t, "security_users")
	attackPayloads := []string{
		`' OR 1=1 --`,
		`'; DROP TABLE security_users; --`,
		`admin'/*`,
		`' UNION SELECT current_user, current_database(), true --`,
		`\'; SELECT pg_sleep(10); --`,
		`" OR "1"="1`,
		"line1\n' OR true --",
		"nul-free unicode 💣 ' ; --",
	}

	for _, payload := range attackPayloads {
		payload := payload
		t.Run(testName(payload), func(t *testing.T) {
			statement, err := Select(table).
				Where(columns.Email.Eq(payload)).
				Limit(1).
				Build()
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(statement.SQL, payload) {
				t.Fatalf("untrusted value was interpolated into SQL: %q", statement.SQL)
			}
			if !strings.Contains(statement.SQL, "$1") {
				t.Fatalf("SQL does not contain a positional placeholder: %q", statement.SQL)
			}
			if strings.Contains(statement.SQL, "DROP TABLE") || strings.Contains(statement.SQL, "UNION SELECT") || strings.Contains(statement.SQL, "pg_sleep") {
				t.Fatalf("attack syntax changed SQL structure: %q", statement.SQL)
			}
			if !reflect.DeepEqual(statement.Args, []any{payload, int64(1)}) {
				t.Fatalf("arguments = %#v, want exact payload and limit", statement.Args)
			}
		})
	}
}

func TestTypedValuePayloadDoesNotChangeStatementShape(t *testing.T) {
	table, columns := benchmarkUserMetadata(t, "security_users")
	build := func(value string) Statement {
		t.Helper()
		statement, err := Select(table).
			Where(And(columns.Email.Eq(value), columns.Active.Eq(true))).
			OrderBy(columns.ID.Asc()).
			Limit(10).
			Build()
		if err != nil {
			t.Fatal(err)
		}
		return statement
	}

	baseline := build("ordinary@example.com")
	for _, payload := range []string{
		`' OR 1=1 --`,
		`'; DELETE FROM security_users; --`,
		`' UNION ALL SELECT NULL --`,
	} {
		statement := build(payload)
		if statement.SQL != baseline.SQL {
			t.Fatalf("payload changed statement shape:\nbaseline: %s\nattack:   %s", baseline.SQL, statement.SQL)
		}
		if got, ok := statement.Args[0].(string); !ok || got != payload {
			t.Fatalf("first argument = %#v, want exact payload", statement.Args[0])
		}
	}
}

func testName(payload string) string {
	replacer := strings.NewReplacer(
		" ", "_",
		"'", "quote",
		`"`, "double_quote",
		";", "semicolon",
		"/", "slash",
		"\\", "backslash",
		"\n", "newline",
	)
	name := replacer.Replace(payload)
	if len(name) > 80 {
		name = name[:80]
	}
	return name
}
