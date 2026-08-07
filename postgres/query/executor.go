package query

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"
)

// DB is the standard database/sql execution surface implemented by sql.DB,
// sql.Conn, and sql.Tx. Driver-specific types are intentionally excluded from
// the public query runtime contract.
type DB interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// Result is the driver-neutral outcome of a non-returning mutation.
type Result struct {
	RowsAffected int64
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

// Observer receives best-effort execution events. Implementations may be called
// concurrently. Observer panics are recovered and ignored so instrumentation
// cannot change query semantics.
type Observer interface {
	Before(context.Context, Event)
	After(context.Context, Event)
}

// Executor executes built statements through database/sql.
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
		if isNilInterface(observer) {
			return fmt.Errorf("query: observer is nil")
		}
		executor.observer = observer
		return nil
	}
}

// NewExecutor validates and creates a reusable executor.
func NewExecutor(db DB, options ...ExecutorOption) (*Executor, error) {
	if isNilInterface(db) {
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

// ExecutionError adds operation and SQL context without formatting arguments or
// raw driver error text. Use errors.Is, errors.As, or Unwrap to inspect the cause.
type ExecutionError struct {
	Operation string
	SQL       string
	Err       error
}

func (e *ExecutionError) Error() string {
	if e == nil {
		return "query: execution failed"
	}
	return fmt.Sprintf("query: %s failed for SQL %q", e.Operation, e.SQL)
}

func (e *ExecutionError) Unwrap() error { return e.Err }

// ErrMultipleRows indicates that an exactly-one RETURNING helper received more
// than one row. The database mutation has already executed; use a transaction
// when the caller needs this error to roll the mutation back.
var ErrMultipleRows = errors.New("query: expected exactly one returned row")

// Exec executes a non-returning statement.
func (e *Executor) Exec(ctx context.Context, operation string, statement Statement) (Result, error) {
	if err := e.validate(operation, statement); err != nil {
		return Result{}, err
	}
	started := e.start(ctx, operation, statement.SQL)
	sqlResult, err := e.db.ExecContext(ctx, statement.SQL, statement.Args...)
	if err != nil {
		e.finish(ctx, operation, statement.SQL, started, 0, err)
		return Result{}, executionError(operation, statement.SQL, err)
	}
	if sqlResult == nil {
		err = errors.New("query: database returned nil result")
		e.finish(ctx, operation, statement.SQL, started, 0, err)
		return Result{}, executionError(operation+" result", statement.SQL, err)
	}
	rowsAffected, rowsErr := sqlResult.RowsAffected()
	if rowsErr != nil {
		e.finish(ctx, operation, statement.SQL, started, 0, rowsErr)
		return Result{}, executionError(operation+" result", statement.SQL, rowsErr)
	}
	result := Result{RowsAffected: rowsAffected}
	e.finish(ctx, operation, statement.SQL, started, rowsAffected, nil)
	return result, nil
}

// FetchAll executes a typed SELECT and scans every row without reflection.
func FetchAll[T any](ctx context.Context, executor *Executor, builder SelectBuilder[T]) ([]T, error) {
	statement, err := builder.Build()
	return scanAll(ctx, executor, "select all", builder.table, statement, err)
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
	if builder.table == nil {
		return zero, false, fmt.Errorf("query: %s requires table metadata", operation)
	}

	started := executor.start(ctx, operation, statement.SQL)
	row := executor.db.QueryRowContext(ctx, statement.SQL, statement.Args...)
	if row == nil {
		err := errors.New("query: database returned nil row")
		executor.finish(ctx, operation, statement.SQL, started, 0, err)
		return zero, false, executionError(operation, statement.SQL, err)
	}
	value, err := builder.table.Scan(row)
	if err != nil {
		if optional && errors.Is(err, sql.ErrNoRows) {
			executor.finish(ctx, operation, statement.SQL, started, 0, nil)
			return zero, false, nil
		}
		executor.finish(ctx, operation, statement.SQL, started, 0, err)
		return zero, false, executionError(operation, statement.SQL, err)
	}
	executor.finish(ctx, operation, statement.SQL, started, 1, nil)
	return value, true, nil
}

// InsertOne executes a single-row INSERT ... RETURNING and requires exactly one row.
func InsertOne[T any](ctx context.Context, executor *Executor, builder InsertBuilder[T]) (T, error) {
	var zero T
	if len(builder.rows) != 1 {
		return zero, fmt.Errorf("query: insert returning one requires exactly one input row")
	}
	statement, err := builder.Returning().Build()
	return returningExactlyOne(ctx, executor, "insert returning one", builder.table, statement, err)
}

// InsertAll executes INSERT ... RETURNING and scans every returned row.
func InsertAll[T any](ctx context.Context, executor *Executor, builder InsertBuilder[T]) ([]T, error) {
	statement, err := builder.Returning().Build()
	return scanAll(ctx, executor, "insert returning all", builder.table, statement, err)
}

// UpdateOne executes UPDATE ... RETURNING and requires exactly one returned row.
func UpdateOne[T any](ctx context.Context, executor *Executor, builder UpdateBuilder[T]) (T, error) {
	statement, err := builder.Returning().Build()
	return returningExactlyOne(ctx, executor, "update returning one", builder.table, statement, err)
}

// UpdateAll executes UPDATE ... RETURNING and scans every returned row.
func UpdateAll[T any](ctx context.Context, executor *Executor, builder UpdateBuilder[T]) ([]T, error) {
	statement, err := builder.Returning().Build()
	return scanAll(ctx, executor, "update returning all", builder.table, statement, err)
}

// DeleteOne executes DELETE ... RETURNING and requires exactly one returned row.
func DeleteOne[T any](ctx context.Context, executor *Executor, builder DeleteBuilder[T]) (T, error) {
	statement, err := builder.Returning().Build()
	return returningExactlyOne(ctx, executor, "delete returning one", builder.table, statement, err)
}

// DeleteAll executes DELETE ... RETURNING and scans every returned row.
func DeleteAll[T any](ctx context.Context, executor *Executor, builder DeleteBuilder[T]) ([]T, error) {
	statement, err := builder.Returning().Build()
	return scanAll(ctx, executor, "delete returning all", builder.table, statement, err)
}

func scanAll[T any](ctx context.Context, executor *Executor, operation string, table *Table[T], statement Statement, buildErr error) ([]T, error) {
	if buildErr != nil {
		return nil, buildErr
	}
	if executor == nil {
		return nil, fmt.Errorf("query: %s requires an executor", operation)
	}
	if table == nil {
		return nil, fmt.Errorf("query: %s requires table metadata", operation)
	}
	if err := executor.validate(operation, statement); err != nil {
		return nil, err
	}

	started := executor.start(ctx, operation, statement.SQL)
	rows, err := executor.db.QueryContext(ctx, statement.SQL, statement.Args...)
	if err != nil {
		if rows != nil {
			_ = rows.Close()
		}
		executor.finish(ctx, operation, statement.SQL, started, 0, err)
		return nil, executionError(operation, statement.SQL, err)
	}
	if rows == nil {
		err := errors.New("query: database returned nil rows")
		executor.finish(ctx, operation, statement.SQL, started, 0, err)
		return nil, executionError(operation, statement.SQL, err)
	}
	defer rows.Close()

	result := make([]T, 0)
	for rows.Next() {
		value, scanErr := table.Scan(rows)
		if scanErr != nil {
			_ = rows.Close()
			executor.finish(ctx, operation, statement.SQL, started, int64(len(result)), scanErr)
			return nil, executionError(operation+" scan", statement.SQL, scanErr)
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		executor.finish(ctx, operation, statement.SQL, started, int64(len(result)), err)
		return nil, executionError(operation+" rows", statement.SQL, err)
	}
	executor.finish(ctx, operation, statement.SQL, started, int64(len(result)), nil)
	return result, nil
}

func returningExactlyOne[T any](ctx context.Context, executor *Executor, operation string, table *Table[T], statement Statement, buildErr error) (T, error) {
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
	rows, err := executor.db.QueryContext(ctx, statement.SQL, statement.Args...)
	if err != nil {
		if rows != nil {
			_ = rows.Close()
		}
		executor.finish(ctx, operation, statement.SQL, started, 0, err)
		return zero, executionError(operation, statement.SQL, err)
	}
	if rows == nil {
		err := errors.New("query: database returned nil rows")
		executor.finish(ctx, operation, statement.SQL, started, 0, err)
		return zero, executionError(operation, statement.SQL, err)
	}
	defer rows.Close()

	if !rows.Next() {
		rowErr := rows.Err()
		if rowErr == nil {
			rowErr = sql.ErrNoRows
		}
		executor.finish(ctx, operation, statement.SQL, started, 0, rowErr)
		return zero, executionError(operation, statement.SQL, rowErr)
	}
	value, scanErr := table.Scan(rows)
	if scanErr != nil {
		executor.finish(ctx, operation, statement.SQL, started, 0, scanErr)
		return zero, executionError(operation+" scan", statement.SQL, scanErr)
	}
	if rows.Next() {
		executor.finish(ctx, operation, statement.SQL, started, 2, ErrMultipleRows)
		return zero, executionError(operation, statement.SQL, ErrMultipleRows)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		executor.finish(ctx, operation, statement.SQL, started, 1, rowsErr)
		return zero, executionError(operation+" rows", statement.SQL, rowsErr)
	}
	executor.finish(ctx, operation, statement.SQL, started, 1, nil)
	return value, nil
}

// ExecInsert executes a non-returning INSERT or UPSERT.
func ExecInsert[T any](ctx context.Context, executor *Executor, builder InsertBuilder[T]) (Result, error) {
	statement, err := builder.Build()
	if err != nil {
		return Result{}, err
	}
	return executor.Exec(ctx, "insert", statement)
}

// ExecUpdate executes a non-returning UPDATE.
func ExecUpdate[T any](ctx context.Context, executor *Executor, builder UpdateBuilder[T]) (Result, error) {
	statement, err := builder.Build()
	if err != nil {
		return Result{}, err
	}
	return executor.Exec(ctx, "update", statement)
}

// ExecDelete executes a non-returning DELETE.
func ExecDelete[T any](ctx context.Context, executor *Executor, builder DeleteBuilder[T]) (Result, error) {
	statement, err := builder.Build()
	if err != nil {
		return Result{}, err
	}
	return executor.Exec(ctx, "delete", statement)
}

func (e *Executor) validate(operation string, statement Statement) error {
	if e == nil || isNilInterface(e.db) {
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

func executionError(operation, sqlText string, err error) error {
	return &ExecutionError{Operation: operation, SQL: sqlText, Err: err}
}

func (e *Executor) start(ctx context.Context, operation, sqlText string) time.Time {
	started := e.now()
	e.observeBefore(ctx, Event{Operation: operation, SQL: sqlText, StartedAt: started})
	return started
}

func (e *Executor) finish(ctx context.Context, operation, sqlText string, started time.Time, rowsAffected int64, err error) {
	eventErr := err
	if err != nil {
		eventErr = executionError(operation, sqlText, err)
	}
	e.observeAfter(ctx, Event{
		Operation:    operation,
		SQL:          sqlText,
		StartedAt:    started,
		Duration:     nonNegativeDuration(e.now().Sub(started)),
		RowsAffected: rowsAffected,
		Err:          eventErr,
	})
}

func (e *Executor) observeBefore(ctx context.Context, event Event) {
	if e == nil || isNilInterface(e.observer) {
		return
	}
	defer func() { _ = recover() }()
	e.observer.Before(ctx, event)
}

func (e *Executor) observeAfter(ctx context.Context, event Event) {
	if e == nil || isNilInterface(e.observer) {
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

func isNilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
