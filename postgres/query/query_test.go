package query

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
)

type testUser struct {
	ID     int64
	Email  string
	Name   string
	Active bool
}

type userColumns struct {
	ID     Column[testUser, int64]
	Email  Column[testUser, string]
	Name   Column[testUser, string]
	Active Column[testUser, bool]
	Search Column[testUser, string]
}

func testUserMetadata(t *testing.T, tableName string) (*Table[testUser], userColumns) {
	t.Helper()
	table, err := NewTable[testUser](tableName, []string{"id", "email", "name", "active", "search"}, func(RowScanner) (testUser, error) {
		return testUser{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	mustColumn := func(name string, options ...ColumnOptions) Column[testUser, string] {
		column, err := NewColumn[testUser, string](table, name, options...)
		if err != nil {
			t.Fatal(err)
		}
		return column
	}
	id, err := NewColumn[testUser, int64](table, "id", InsertOnlyColumn())
	if err != nil {
		t.Fatal(err)
	}
	active, err := NewColumn[testUser, bool](table, "active")
	if err != nil {
		t.Fatal(err)
	}
	return table, userColumns{
		ID:     id,
		Email:  mustColumn("email"),
		Name:   mustColumn("name"),
		Active: active,
		Search: mustColumn("search", ReadOnlyColumn()),
	}
}

func TestSelectBuildsParameterizedPostgreSQL(t *testing.T) {
	t.Parallel()
	table, columns := testUserMetadata(t, "users")
	injection := `x' OR TRUE --`

	statement, err := Select(table).
		Distinct().
		Where(And(columns.Email.Eq(injection), columns.ID.In(7, 9))).
		OrderBy(columns.ID.Desc(), columns.Email.Asc()).
		Limit(20).
		Offset(40).
		ForUpdate().
		SkipLocked().
		Build()
	if err != nil {
		t.Fatal(err)
	}
	wantSQL := `SELECT DISTINCT "id", "email", "name", "active", "search" FROM "users" WHERE ("email" = $1) AND ("id" IN ($2, $3)) ORDER BY "id" DESC, "email" ASC LIMIT $4 OFFSET $5 FOR UPDATE SKIP LOCKED`
	if statement.SQL != wantSQL {
		t.Fatalf("SQL = %q\nwant = %q", statement.SQL, wantSQL)
	}
	wantArgs := []any{injection, int64(7), int64(9), int64(20), int64(40)}
	if !reflect.DeepEqual(statement.Args, wantArgs) {
		t.Fatalf("Args = %#v, want %#v", statement.Args, wantArgs)
	}
	if strings.Contains(statement.SQL, injection) {
		t.Fatal("injection-shaped value was interpolated into SQL")
	}
}

func TestSelectPredicateCompositionAndValidation(t *testing.T) {
	t.Parallel()
	table, columns := testUserMetadata(t, "users")
	otherTable, otherColumns := testUserMetadata(t, "archived_users")

	statement, err := Select(table).
		Where(Or(Not(columns.Active.Eq(false)), columns.Name.IsNull())).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	want := `SELECT "id", "email", "name", "active", "search" FROM "users" WHERE (NOT ("active" = $1)) OR ("name" IS NULL)`
	if statement.SQL != want || !reflect.DeepEqual(statement.Args, []any{false}) {
		t.Fatalf("unexpected statement: %#v", statement)
	}

	if _, err := Select(table).Where(otherColumns.Email.Eq("x")).Build(); err == nil {
		t.Fatal("expected cross-table predicate error")
	}
	if _, err := Select(otherTable).OrderBy(columns.Email.Asc()).Build(); err == nil {
		t.Fatal("expected cross-table order error")
	}
	if _, err := Select(table).Where(columns.ID.In()).Build(); err == nil {
		t.Fatal("expected empty IN error")
	}
	if _, err := Select(table).Where(And[testUser]()).Build(); err == nil {
		t.Fatal("expected empty AND error")
	}
	if _, err := Select(table).SkipLocked().Build(); err == nil {
		t.Fatal("expected lock-wait-without-lock error")
	}
	if _, err := Select(table).ForUpdate().ForShare().Build(); err == nil {
		t.Fatal("expected conflicting lock error")
	}
}

func TestTrustedPredicateIsExplicit(t *testing.T) {
	t.Parallel()
	table, _ := testUserMetadata(t, "users")
	statement, err := Select(table).Where(TrustedPredicate(table, TrustedSQL(`lower("email") = current_user`))).Build()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(statement.SQL, `lower("email") = current_user`) || len(statement.Args) != 0 {
		t.Fatalf("unexpected trusted predicate: %#v", statement)
	}
	if _, err := Select(table).Where(TrustedPredicate(table, TrustedSQL(""))).Build(); err == nil {
		t.Fatal("expected empty trusted SQL error")
	}
}

func TestInsertMultiRowAndUpsert(t *testing.T) {
	t.Parallel()
	table, columns := testUserMetadata(t, "users")

	multi, err := Insert(table).
		Row(columns.ID.Set(1), columns.Email.Set("one@example.com"), columns.Name.Set("One"), columns.Active.Set(true)).
		Row(columns.Active.Set(false), columns.Name.Set("Two"), columns.Email.Set("two@example.com"), columns.ID.Set(2)).
		Returning().
		Build()
	if err != nil {
		t.Fatal(err)
	}
	wantMulti := `INSERT INTO "users" ("active", "email", "id", "name") VALUES ($1, $2, $3, $4), ($5, $6, $7, $8) RETURNING "id", "email", "name", "active", "search"`
	if multi.SQL != wantMulti {
		t.Fatalf("multi SQL = %q\nwant = %q", multi.SQL, wantMulti)
	}
	wantArgs := []any{true, "one@example.com", int64(1), "One", false, "two@example.com", int64(2), "Two"}
	if !reflect.DeepEqual(multi.Args, wantArgs) {
		t.Fatalf("multi args = %#v, want %#v", multi.Args, wantArgs)
	}

	upsert, err := Insert(table).
		Row(columns.ID.Set(7), columns.Email.Set("new@example.com"), columns.Name.Set("New"), columns.Active.Set(true)).
		OnConflict(columns.ID.ConflictTarget()).
		DoUpdate(columns.Email.Excluded(), columns.Name.SetSQL(TrustedSQL(`upper(EXCLUDED."name")`)), columns.Active.Set(false)).
		Returning().
		Build()
	if err != nil {
		t.Fatal(err)
	}
	wantUpsert := `INSERT INTO "users" ("active", "email", "id", "name") VALUES ($1, $2, $3, $4) ON CONFLICT ("id") DO UPDATE SET "active" = $5, "email" = EXCLUDED."email", "name" = upper(EXCLUDED."name") RETURNING "id", "email", "name", "active", "search"`
	if upsert.SQL != wantUpsert {
		t.Fatalf("upsert SQL = %q\nwant = %q", upsert.SQL, wantUpsert)
	}
	wantUpsertArgs := []any{true, "new@example.com", int64(7), "New", false}
	if !reflect.DeepEqual(upsert.Args, wantUpsertArgs) {
		t.Fatalf("upsert args = %#v, want %#v", upsert.Args, wantUpsertArgs)
	}

	doNothing, err := Insert(table).Row(columns.ID.Set(8)).OnAnyConflict().DoNothing().Build()
	if err != nil {
		t.Fatal(err)
	}
	if doNothing.SQL != `INSERT INTO "users" ("id") VALUES ($1) ON CONFLICT DO NOTHING` {
		t.Fatalf("unexpected DO NOTHING SQL: %s", doNothing.SQL)
	}
}

func TestInsertRejectsInvalidRowsAndColumns(t *testing.T) {
	t.Parallel()
	table, columns := testUserMetadata(t, "users")
	otherTable, otherColumns := testUserMetadata(t, "other_users")

	tests := []struct {
		name  string
		build func() error
	}{
		{"no rows", func() error { _, err := Insert(table).Build(); return err }},
		{"different columns", func() error {
			_, err := Insert(table).Row(columns.ID.Set(1), columns.Email.Set("a")).Row(columns.ID.Set(2)).Build()
			return err
		}},
		{"duplicate column", func() error {
			_, err := Insert(table).Row(columns.ID.Set(1), columns.ID.Set(2)).Build()
			return err
		}},
		{"read only", func() error {
			_, err := Insert(table).Row(columns.Search.Set("x")).Build()
			return err
		}},
		{"cross table assignment", func() error {
			_, err := Insert(table).Row(otherColumns.Email.Set("x")).Build()
			return err
		}},
		{"cross table conflict", func() error {
			_, err := Insert(table).Row(columns.ID.Set(1)).OnConflict(otherColumns.ID.ConflictTarget()).DoNothing().Build()
			return err
		}},
		{"excluded row", func() error {
			_, err := Insert(table).Row(columns.Email.Excluded()).Build()
			return err
		}},
		{"targetless update", func() error {
			_, err := Insert(table).Row(columns.ID.Set(1)).OnAnyConflict().DoUpdate(columns.Email.Set("x")).Build()
			return err
		}},
	}
	_ = otherTable
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := test.build(); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestUpdateDeleteSafetyAndReturning(t *testing.T) {
	t.Parallel()
	table, columns := testUserMetadata(t, "users")

	update, err := Update(table).
		Set(columns.Name.Set("Updated"), columns.Active.Set(false)).
		Where(columns.ID.Eq(9)).
		Returning().
		Build()
	if err != nil {
		t.Fatal(err)
	}
	wantUpdate := `UPDATE "users" SET "active" = $1, "name" = $2 WHERE "id" = $3 RETURNING "id", "email", "name", "active", "search"`
	if update.SQL != wantUpdate || !reflect.DeepEqual(update.Args, []any{false, "Updated", int64(9)}) {
		t.Fatalf("unexpected update: %#v", update)
	}

	deleteStatement, err := Delete(table).Where(columns.ID.Eq(9)).Returning().Build()
	if err != nil {
		t.Fatal(err)
	}
	wantDelete := `DELETE FROM "users" WHERE "id" = $1 RETURNING "id", "email", "name", "active", "search"`
	if deleteStatement.SQL != wantDelete || !reflect.DeepEqual(deleteStatement.Args, []any{int64(9)}) {
		t.Fatalf("unexpected delete: %#v", deleteStatement)
	}

	if _, err := Update(table).Set(columns.Name.Set("x")).Build(); err == nil {
		t.Fatal("expected unsafe UPDATE error")
	}
	if _, err := Delete(table).Build(); err == nil {
		t.Fatal("expected unsafe DELETE error")
	}
	if _, err := Update(table).Set(columns.Name.Set("x")).AllRows().Where(columns.ID.Eq(1)).Build(); err == nil {
		t.Fatal("expected UPDATE WHERE/AllRows conflict")
	}
	if _, err := Update(table).Set(columns.ID.Set(3)).AllRows().Build(); err == nil {
		t.Fatal("expected insert-only update error")
	}
	if _, err := Update(table).Set(columns.Search.Set("x")).AllRows().Build(); err == nil {
		t.Fatal("expected read-only update error")
	}

	allRows, err := Delete(table).AllRows().Build()
	if err != nil {
		t.Fatal(err)
	}
	if allRows.SQL != `DELETE FROM "users"` || len(allRows.Args) != 0 {
		t.Fatalf("unexpected all-rows delete: %#v", allRows)
	}
}

func TestBuildersAreImmutableAndSafeForConcurrentReuse(t *testing.T) {
	t.Parallel()
	table, columns := testUserMetadata(t, "users")
	base := Select(table).Where(columns.Active.Eq(true)).OrderBy(columns.ID.Asc())

	const workers = 32
	var wait sync.WaitGroup
	errors := make(chan error, workers)
	for index := 0; index < workers; index++ {
		index := index
		wait.Add(1)
		go func() {
			defer wait.Done()
			statement, err := base.Limit(int64(index + 1)).Offset(int64(index)).Build()
			if err != nil {
				errors <- err
				return
			}
			if !reflect.DeepEqual(statement.Args, []any{true, int64(index + 1), int64(index)}) {
				errors <- fmt.Errorf("worker %d args = %#v", index, statement.Args)
			}
		}()
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}

	baseStatement, err := base.Build()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(baseStatement.SQL, "LIMIT") || !reflect.DeepEqual(baseStatement.Args, []any{true}) {
		t.Fatalf("base builder was mutated: %#v", baseStatement)
	}
}

func TestMetadataValidation(t *testing.T) {
	t.Parallel()
	if _, err := NewTable[testUser]("", []string{"id"}, func(RowScanner) (testUser, error) { return testUser{}, nil }); err == nil {
		t.Fatal("expected empty table error")
	}
	if _, err := NewTable[testUser]("users", []string{"id", "id"}, func(RowScanner) (testUser, error) { return testUser{}, nil }); err == nil {
		t.Fatal("expected duplicate projection error")
	}
	if _, err := NewTable[testUser]("users", []string{"id"}, nil); err == nil {
		t.Fatal("expected nil scanner error")
	}
}
