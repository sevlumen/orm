package v1example

import (
	"reflect"
	"strings"
	"testing"

	"github.com/sevlumen/orm/postgres/query"
)

func TestCRUDStatementsAreParameterized(t *testing.T) {
	payload := `person'; DROP TABLE users; --`
	insert, selectOne, update, deleteOne, err := CRUDStatements(payload)
	if err != nil {
		t.Fatal(err)
	}
	for name, statement := range map[string]query.Statement{
		"insert": insert,
		"select": selectOne,
		"update": update,
		"delete": deleteOne,
	} {
		if strings.Contains(statement.SQL, payload) {
			t.Fatalf("%s interpolated payload into SQL: %s", name, statement.SQL)
		}
		if !strings.Contains(statement.SQL, "$1") {
			t.Fatalf("%s has no positional placeholder: %s", name, statement.SQL)
		}
	}
	if !reflect.DeepEqual(insert.Args, []any{int64(1), payload, true}) {
		t.Fatalf("insert args=%#v", insert.Args)
	}
	if !reflect.DeepEqual(selectOne.Args, []any{payload, int64(1)}) {
		t.Fatalf("select args=%#v", selectOne.Args)
	}
	if !reflect.DeepEqual(update.Args, []any{false, payload}) {
		t.Fatalf("update args=%#v", update.Args)
	}
	if !reflect.DeepEqual(deleteOne.Args, []any{payload}) {
		t.Fatalf("delete args=%#v", deleteOne.Args)
	}
}

func TestRelationAndObserverExamplesConstruct(t *testing.T) {
	_ = UserOrders()
	event := query.Event{Operation: "select", RowsAffected: 2}
	if got := EventSummary(event); !strings.Contains(got, "operation=select") || !strings.Contains(got, "rows=2") {
		t.Fatalf("summary=%q", got)
	}
}
