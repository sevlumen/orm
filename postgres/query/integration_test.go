package query

import (
	"context"
	"fmt"
	"testing"
	"time"
)

type integrationUser struct {
	ID     int64
	Email  string
	Active bool
}

func TestTypedBuildersAgainstPostgreSQL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	db := openIntegrationDatabase(t)

	tableName := fmt.Sprintf("sl_query_users_%x", time.Now().UnixNano())
	qualified := quoteIdentifier(tableName)
	if _, err := db.ExecContext(ctx, "CREATE TABLE "+qualified+" (id bigint PRIMARY KEY, email text UNIQUE NOT NULL, active boolean NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanupCtx, "DROP TABLE IF EXISTS "+qualified)
	}()

	table, err := NewTable[integrationUser](tableName, []string{"id", "email", "active"}, func(row RowScanner) (integrationUser, error) {
		var value integrationUser
		err := row.Scan(&value.ID, &value.Email, &value.Active)
		return value, err
	})
	if err != nil {
		t.Fatal(err)
	}
	id, err := NewColumn[integrationUser, int64](table, "id", InsertOnlyColumn())
	if err != nil {
		t.Fatal(err)
	}
	email, err := NewColumn[integrationUser, string](table, "email")
	if err != nil {
		t.Fatal(err)
	}
	active, err := NewColumn[integrationUser, bool](table, "active")
	if err != nil {
		t.Fatal(err)
	}

	injectionShapedEmail := `first' OR TRUE --@example.com`
	insertStatement, err := Insert(table).
		Row(id.Set(1), email.Set(injectionShapedEmail), active.Set(true)).
		Returning().
		Build()
	if err != nil {
		t.Fatal(err)
	}
	inserted, err := table.Scan(db.QueryRowContext(ctx, insertStatement.SQL, insertStatement.Args...))
	if err != nil {
		t.Fatal(err)
	}
	if inserted.ID != 1 || inserted.Email != injectionShapedEmail || !inserted.Active {
		t.Fatalf("unexpected inserted row: %#v", inserted)
	}

	selectStatement, err := Select(table).
		Where(And(id.Eq(1), email.Eq(injectionShapedEmail))).
		ForShare().
		Build()
	if err != nil {
		t.Fatal(err)
	}
	selected, err := table.Scan(db.QueryRowContext(ctx, selectStatement.SQL, selectStatement.Args...))
	if err != nil {
		t.Fatal(err)
	}
	if selected != inserted {
		t.Fatalf("selected = %#v, inserted = %#v", selected, inserted)
	}

	upsertStatement, err := Insert(table).
		Row(id.Set(1), email.Set("updated@example.com"), active.Set(false)).
		OnConflict(id.ConflictTarget()).
		DoUpdate(email.Excluded(), active.Excluded()).
		Returning().
		Build()
	if err != nil {
		t.Fatal(err)
	}
	upserted, err := table.Scan(db.QueryRowContext(ctx, upsertStatement.SQL, upsertStatement.Args...))
	if err != nil {
		t.Fatal(err)
	}
	if upserted.Email != "updated@example.com" || upserted.Active {
		t.Fatalf("unexpected upserted row: %#v", upserted)
	}

	updateStatement, err := Update(table).
		Set(active.Set(true)).
		Where(id.Eq(1)).
		Returning().
		Build()
	if err != nil {
		t.Fatal(err)
	}
	updated, err := table.Scan(db.QueryRowContext(ctx, updateStatement.SQL, updateStatement.Args...))
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Active {
		t.Fatalf("update did not set active: %#v", updated)
	}

	deleteStatement, err := Delete(table).
		Where(id.Eq(1)).
		Returning().
		Build()
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := table.Scan(db.QueryRowContext(ctx, deleteStatement.SQL, deleteStatement.Args...))
	if err != nil {
		t.Fatal(err)
	}
	if deleted.ID != 1 {
		t.Fatalf("unexpected deleted row: %#v", deleted)
	}

	var remaining int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM "+qualified).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("remaining rows = %d", remaining)
	}
}
