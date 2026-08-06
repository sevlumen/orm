package query

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// DB is the shared execution surface implemented by pgxpool.Pool and pgx.Tx.
type DB interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

// Event describes one database operation without exposing argument values.
type Event struct {
	Operation    string
	SQL          string
	StartedAt    time.Time
	Duration     time.Duration
	RowsAffected int64
	Err          error
}

// Observer receives best-effort execution events. Observer panics are recovered
// and ignored so instrumentation cannot change query semantics.
type Observer interface {
	Before(context.Context, Event)
	After(context.Context, Event)
}

// Executor executes built statements through pgx.
type Executor struct {
	db       DB
	observer Observer
	now      func() time.Time
}

// ExecutorOption configures an Executor.
type ExecutorOption func(*Executor) error

// WithObserver installs optional best-effort execution instrumentation.
func WithObserver(observer Observer) ExecutorOption {
	return func(executor *Executor) error {
		if observer == nil {
			return fmt.Errorf("query: observer is nil")
		}
		executor.observer = observer
		return nil
	}
}

// NewExecutor validates and creates a reusable executor.
func NewExecutor(db DB, options ...ExecutorOption) (*Executor, error) {
	if db == nil {
		return nil, fmt.Errorf("query: executor requires a database")
	}
	executor := &Executor{db: db, now: time.Now}
	for index, option := range options {
		if option == nil {
			return nil, fmt.Errorf("query: executor option %d is nil", index)
		}
		if err := option(executor); err != nil {
			return nil, err
		}
	}
	return executor, nil
}

// ExecutionError adds operation and SQL context without formatting arguments.
type ExecutionError struct {
	Operation string
	SQL       string
	Err       error
}

func (e *ExecutionError) Error() string {
	return fmt.Sprintf("query: %s failed for SQL %q: %v", e.Operation, e.SQL, e.Err)
}

func (e *ExecutionError) Unwrap() error { return e.Err }

// Exec executes a non-returning statement.
func (e *Executor) Exec(ctx context.Context, operation string, statement Statement) (pgconn.CommandTag, error) {
	if err := e.validate(operation, statement); err != nil {
		return pgconn.CommandTag{}, err
	}
	started := e.start(ctx, operation, statement.SQL)
	tag, err := e.db.Exec(ctx, statement.SQL, statement.Args...)
	e.finish(ctx, operation, statement.SQL, started, tag.RowsAffected(), err)
	if err != nil {
		return pgconn.CommandTag{}, executionError(operation, statement.SQL, err)
	}
	return tag, nil
}

// FetchAll executes a typed SELECT and scans every row without reflection.
func FetchAll[T any](ctx context.Context, executor *Executor, builder SelectBuilder[T]) ([]T, error) {
	if executor == nil {
		return nil, fmt.Errorf("query: FetchAll requires an executor")
	}
	statement, err := builder.Build()
	if err != nil {
		return nil, err
	}
	if err := executor.validate("select all", statement); err != nil {
		return nil, err
	}

	started := executor.start(ctx, "select all", statement.SQL)
	rows, err := executor.db.Query(ctx, statement.SQL, statement.Args...)
	if err != nil {
		executor.finish(ctx, "select all", statement.SQL, started, 0, err)
		return nil, executionError("select all", statement.SQL, err)
	}
	defer rows.Close()

	result := make([]T, 0)
	for rows.Next() {
		value, scanErr := builder.table.Scan(rows)
		if scanErr != nil {
			rows.Close()
			executor.finish(ctx, "select all", statement.SQL, started, int64(len(result)), scanErr)
			return nil, executionError("select all scan", statement.SQL, scanErr)
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		executor.finish(ctx, "select all", statement.SQL, started, int64(len(result)), err)
		return nil, executionError("select all rows", statement.SQL, err)
	}
	executor.finish(ctx, "select all", statement.SQL, started, int64(len(result)), nil)
	return result, nil
}

// FetchOne executes a typed SELECT with LIMIT 1 and requires one row.
func FetchOne[T any](ctx context.Context, executor *Executor, builder SelectBuilder[T]) (T, error) {
	value, _, err := fetchOne(ctx, executor, builder, false)
	return value, err
}

// FetchOptional executes a typed SELECT with LIMIT 1 and treats no rows as an empty result.
func FetchOptional[T any](ctx context.Context, executor *Executor, builder SelectBuilder[T]) (T, bool, error) {
	return fetchOne(ctx, executor, builder, true)
}

func fetchOne[T any](ctx context.Context, executor *Executor, builder SelectBuilder[T], optional bool) (T, bool, error) {
	var zero T
	if executor == nil {
		return zero, false, fmt.Errorf("query: fetch one requires an executor")
	}
	statement, err := builder.Limit(1).Build()
	if err != nil {
		return zero, false, err
	}
	operation := "select one"
	if err := executor.validate(operation, statement); err != nil {
		return zero, false, err
	}

	started := executor.start(ctx, operation, statement.SQL)
	value, err := builder.table.Scan(executor.db.QueryRow(ctx, statement.SQL, statement.Args...))
	if err != nil {
		if optional && errors.Is(err, pgx.ErrNoRows) {
			executor.finish(ctx, operation, statement.SQL, started, 0, nil)
			return zero, false, nil
		}
		executor.finish(ctx, operation, statement.SQL, started, 0, err)
		return zero, false, executionError(operation, statement.SQL, err)
	}
	executor.finish(ctx, operation, statement.SQL, started, 1, nil)
	return value, true, nil
}

// InsertOne executes INSERT ... RETURNING and scans one row.
func InsertOne[T any](ctx context.Context, executor *Executor, builder InsertBuilder[T]) (T, error) {
	return returningOne(ctx, executor, "insert returning", builder.table, builder.Returning().Build())
}

// UpdateOne executes UPDATE ... RETURNING and scans one row.
func UpdateOne[T any](ctx context.Context, executor *Executor, builder UpdateBuilder[T]) (T, error) {
	return returningOne(ctx, executor, "update returning", builder.table, builder.Returning().Build())
}

// DeleteOne executes DELETE ... RETURNING and scans one row.
func DeleteOne[T any](ctx context.Context, executor *Executor, builder DeleteBuilder[T]) (T, error) {
	return returningOne(ctx, executor, "delete returning", builder.table, builder.Returning().Build())
}

func returningOne[T any](ctx context.Context, executor *Executor, operation string, table *Table[T], statement Statement, buildErr error) (T, error) {
	var zero T
	if buildErr != nil {
		return zero, buildErr
	}
	if executor == nil {
		return zero, fmt.Errorf("query: %s requires an executor", operation)
	}
	if table == nil {
		return zero, fmt.Errorf("query: %s requires table metadata", operation)
	}
	if err := executor.validate(operation, statement); err != nil {
		return zero, err
	}
	started := executor.start(ctx, operation, statement.SQL)
	value, err := table.Scan(executor.db.QueryRow(ctx, statement.SQL, statement.Args...))
	if err != nil {
		executor.finish(ctx, operation, statement.SQL, started, 0, err)
		return zero, executionError(operation, statement.SQL, err)
	}
	executor.finish(ctx, operation, statement.SQL, started, 1, nil)
	return value, nil
}

// ExecInsert executes a non-returning INSERT or UPSERT.
func ExecInsert[T any](ctx context.Context, executor *Executor, builder InsertBuilder[T]) (pgconn.CommandTag, error) {
	statement, err := builder.Build()
	if err != nil {
		return pgconn.CommandTag{}, err
	}
	return executor.Exec(ctx, "insert", statement)
}

// ExecUpdate executes a non-returning UPDATE.
func ExecUpdate[T any](ctx context.Context, executor *Executor, builder UpdateBuilder[T]) (pgconn.CommandTag, error) {
	statement, err := builder.Build()
	if err != nil {
		return pgconn.CommandTag{}, err
	}
	return executor.Exec(ctx, "update", statement)
}

// ExecDelete executes a non-returning DELETE.
func ExecDelete[T any](ctx context.Context, executor *Executor, builder DeleteBuilder[T]) (pgconn.CommandTag, error) {
	statement, err := builder.Build()
	if err != nil {
		return pgconn.CommandTag{}, err
	}
	return executor.Exec(ctx, "delete", statement)
}

func (e *Executor) validate(operation string, statement Statement) error {
	if e == nil || e.db == nil {
		return fmt.Errorf("query: %s requires a configured executor", operation)
	}
	if strings.TrimSpace(operation) == "" {
		return fmt.Errorf("query: execution operation is required")
	}
	if strings.TrimSpace(statement.SQL) == "" {
		return fmt.Errorf("query: %s SQL is empty", operation)
	}
	if strings.ContainsRune(statement.SQL, '\x00') {
		return fmt.Errorf("query: %s SQL contains NUL", operation)
	}
	return nil
}

func executionError(operation, sql string, err error) error {
	return &ExecutionError{Operation: operation, SQL: sql, Err: err}
}

func (e *Executor) start(ctx context.Context, operation, sql string) time.Time {
	started := e.now()
	e.observeBefore(ctx, Event{Operation: operation, SQL: sql, StartedAt: started})
	return started
}

func (e *Executor) finish(ctx context.Context, operation, sql string, started time.Time, rowsAffected int64, err error) {
	e.observeAfter(ctx, Event{
		Operation:    operation,
		SQL:          sql,
		StartedAt:    started,
		Duration:     nonNegativeDuration(e.now().Sub(started)),
		RowsAffected: rowsAffected,
		Err:          err,
	})
}

func (e *Executor) observeBefore(ctx context.Context, event Event) {
	if e == nil || e.observer == nil {
		return
	}
	defer func() { _ = recover() }()
	e.observer.Before(ctx, event)
}

func (e *Executor) observeAfter(ctx context.Context, event Event) {
	if e == nil || e.observer == nil {
		return
	}
	defer func() { _ = recover() }()
	e.observer.After(ctx, event)
}

func nonNegativeDuration(value time.Duration) time.Duration {
	if value < 0 {
		return 0
	}
	return value
}
