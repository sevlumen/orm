package query

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type executorFakeDB struct {
	execErr    error
	queryErr   error
	execCalls  atomic.Int64
	queryCalls atomic.Int64
}

func (db *executorFakeDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	db.execCalls.Add(1)
	return pgconn.CommandTag{}, db.execErr
}

func (db *executorFakeDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	db.queryCalls.Add(1)
	return nil, db.queryErr
}

func (db *executorFakeDB) QueryRow(context.Context, string, ...any) pgx.Row {
	return executorFakeRow{err: db.queryErr}
}

type executorFakeRow struct{ err error }

func (row executorFakeRow) Scan(...any) error { return row.err }

type panicObserver struct {
	before atomic.Int64
	after  atomic.Int64
}

func (observer *panicObserver) Before(context.Context, Event) {
	observer.before.Add(1)
	panic("observer before panic")
}

func (observer *panicObserver) After(context.Context, Event) {
	observer.after.Add(1)
	panic("observer after panic")
}

func TestNewExecutorRejectsTypedNilDependencies(t *testing.T) {
	t.Parallel()

	var database *executorFakeDB
	if _, err := NewExecutor(database); err == nil {
		t.Fatal("expected typed-nil database error")
	}

	var observer *panicObserver
	if _, err := NewExecutor(&executorFakeDB{}, WithObserver(observer)); err == nil {
		t.Fatal("expected typed-nil observer error")
	}
}

func TestExecutionErrorDoesNotFormatArgumentValues(t *testing.T) {
	t.Parallel()

	driverErr := errors.New("driver failure")
	database := &executorFakeDB{execErr: driverErr}
	executor, err := NewExecutor(database)
	if err != nil {
		t.Fatal(err)
	}
	secret := "tenant-secret-that-must-not-be-logged"
	_, err = executor.Exec(context.Background(), "update", Statement{
		SQL:  `UPDATE "users" SET "token" = $1`,
		Args: []any{secret},
	})
	if err == nil {
		t.Fatal("expected execution error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("execution error leaked argument value: %v", err)
	}
	var executionErr *ExecutionError
	if !errors.As(err, &executionErr) {
		t.Fatalf("error type = %T, want *ExecutionError", err)
	}
	if executionErr.Operation != "update" || executionErr.SQL == "" || !errors.Is(err, driverErr) {
		t.Fatalf("unexpected execution error: %#v", executionErr)
	}
}

func TestObserverPanicsCannotChangeExecution(t *testing.T) {
	t.Parallel()

	database := &executorFakeDB{}
	observer := &panicObserver{}
	executor, err := NewExecutor(database, WithObserver(observer))
	if err != nil {
		t.Fatal(err)
	}

	const workers = 32
	var wait sync.WaitGroup
	errorsCh := make(chan error, workers)
	for index := 0; index < workers; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, execErr := executor.Exec(context.Background(), "health check", Statement{SQL: "SELECT 1"})
			if execErr != nil {
				errorsCh <- execErr
			}
		}()
	}
	wait.Wait()
	close(errorsCh)
	for execErr := range errorsCh {
		t.Error(execErr)
	}
	if database.execCalls.Load() != workers {
		t.Fatalf("exec calls = %d, want %d", database.execCalls.Load(), workers)
	}
	if observer.before.Load() != workers || observer.after.Load() != workers {
		t.Fatalf("observer calls before=%d after=%d, want %d each", observer.before.Load(), observer.after.Load(), workers)
	}
}

func TestInsertOneRejectsMultiRowBuilderBeforeExecution(t *testing.T) {
	t.Parallel()

	table, columns := testUserMetadata(t, "users")
	database := &executorFakeDB{}
	executor, err := NewExecutor(database)
	if err != nil {
		t.Fatal(err)
	}

	_, err = InsertOne(context.Background(), executor, Insert(table).
		Row(columns.ID.Set(1), columns.Email.Set("one@example.com"), columns.Name.Set("One"), columns.Active.Set(true)).
		Row(columns.ID.Set(2), columns.Email.Set("two@example.com"), columns.Name.Set("Two"), columns.Active.Set(false)))
	if err == nil {
		t.Fatal("expected multi-row InsertOne error")
	}
	if database.queryCalls.Load() != 0 {
		t.Fatalf("query calls = %d, want 0", database.queryCalls.Load())
	}
}

func TestFetchAllHandlesNilRowsWithoutPanic(t *testing.T) {
	t.Parallel()

	table, _ := testUserMetadata(t, "users")
	database := &executorFakeDB{}
	executor, err := NewExecutor(database)
	if err != nil {
		t.Fatal(err)
	}
	_, err = FetchAll(context.Background(), executor, Select(table))
	if err == nil || !strings.Contains(err.Error(), "nil rows") {
		t.Fatalf("error = %v, want nil rows error", err)
	}
}
