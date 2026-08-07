package query

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

type batchEntry struct {
	operation string
	statement Statement
}

// Batch is an immutable queue of non-returning statements.
type Batch struct {
	entries []batchEntry
	err     error
}

// NewBatch returns an empty immutable mutation batch.
func NewBatch() Batch { return Batch{} }

// QueueExec appends one non-returning statement.
func (b Batch) QueueExec(operation string, statement Statement) Batch {
	if b.err != nil {
		return b
	}
	if strings.TrimSpace(operation) == "" {
		b.err = fmt.Errorf("query: batch operation is required")
		return b
	}
	if strings.TrimSpace(statement.SQL) == "" {
		b.err = fmt.Errorf("query: batch SQL is empty")
		return b
	}
	if strings.ContainsRune(statement.SQL, '\x00') {
		b.err = fmt.Errorf("query: batch SQL contains NUL")
		return b
	}
	copyStatement := Statement{SQL: statement.SQL, Args: append([]any(nil), statement.Args...)}
	b.entries = append(append([]batchEntry(nil), b.entries...), batchEntry{operation: operation, statement: copyStatement})
	return b
}

// Len returns the number of queued entries.
func (b Batch) Len() int { return len(b.entries) }

// QueueInsert appends one non-returning typed INSERT.
func QueueInsert[T any](batch Batch, builder InsertBuilder[T]) Batch {
	if builder.returning {
		batch.err = fmt.Errorf("query: mutation batch INSERT cannot use RETURNING")
		return batch
	}
	statement, err := builder.Build()
	if err != nil {
		batch.err = err
		return batch
	}
	return batch.QueueExec("batch insert", statement)
}

// QueueUpdate appends one non-returning typed UPDATE.
func QueueUpdate[T any](batch Batch, builder UpdateBuilder[T]) Batch {
	if builder.returning {
		batch.err = fmt.Errorf("query: mutation batch UPDATE cannot use RETURNING")
		return batch
	}
	statement, err := builder.Build()
	if err != nil {
		batch.err = err
		return batch
	}
	return batch.QueueExec("batch update", statement)
}

// QueueDelete appends one non-returning typed DELETE.
func QueueDelete[T any](batch Batch, builder DeleteBuilder[T]) Batch {
	if builder.returning {
		batch.err = fmt.Errorf("query: mutation batch DELETE cannot use RETURNING")
		return batch
	}
	statement, err := builder.Build()
	if err != nil {
		batch.err = err
		return batch
	}
	return batch.QueueExec("batch delete", statement)
}

// ExecBatch executes queued statements sequentially. When the Executor is
// backed by sql.DB or sql.Conn, the batch is wrapped in one implicit
// transaction. When it is already backed by sql.Tx, the caller owns the
// surrounding transaction.
func ExecBatch(ctx context.Context, executor *Executor, batch Batch) ([]Result, error) {
	if executor == nil || isNilInterface(executor.db) {
		return nil, fmt.Errorf("query: batch requires a configured executor")
	}
	if batch.err != nil {
		return nil, batch.err
	}
	if len(batch.entries) == 0 {
		return nil, fmt.Errorf("query: batch requires at least one statement")
	}
	for _, entry := range batch.entries {
		if err := executor.validate(entry.operation, entry.statement); err != nil {
			return nil, err
		}
	}

	var results []Result
	err := executor.withImplicitTransaction(ctx, func(batchExecutor *Executor) error {
		results = make([]Result, 0, len(batch.entries))
		for _, entry := range batch.entries {
			result, execErr := batchExecutor.Exec(ctx, entry.operation, entry.statement)
			if execErr != nil {
				return execErr
			}
			results = append(results, result)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return results, nil
}

// FetchBatch executes homogeneous typed SELECT builders sequentially. A pool or
// pinned sql.Conn uses one implicit transaction so all reads share one backend
// and snapshot; an existing sql.Tx is reused directly.
func FetchBatch[T any](ctx context.Context, executor *Executor, builders ...SelectBuilder[T]) ([][]T, error) {
	if executor == nil || isNilInterface(executor.db) {
		return nil, fmt.Errorf("query: select batch requires a configured executor")
	}
	if len(builders) == 0 {
		return nil, fmt.Errorf("query: select batch requires at least one SELECT")
	}
	statements := make([]Statement, len(builders))
	for index, builder := range builders {
		statement, buildErr := builder.Build()
		if buildErr != nil {
			return nil, fmt.Errorf("query: select batch item %d: %w", index, buildErr)
		}
		if validateErr := executor.validate("batch select", statement); validateErr != nil {
			return nil, validateErr
		}
		if builder.table == nil {
			return nil, fmt.Errorf("query: select batch item %d requires table metadata", index)
		}
		statements[index] = statement
	}

	var result [][]T
	err := executor.withImplicitTransaction(ctx, func(batchExecutor *Executor) error {
		result = make([][]T, len(builders))
		for index, builder := range builders {
			values, fetchErr := scanAll(ctx, batchExecutor, "batch select", builder.table, statements[index], nil)
			if fetchErr != nil {
				return fetchErr
			}
			result[index] = values
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (e *Executor) withImplicitTransaction(ctx context.Context, callback func(*Executor) error) (err error) {
	if _, alreadyTransaction := e.db.(*sql.Tx); alreadyTransaction {
		return callback(e)
	}
	beginner, ok := e.db.(Beginner)
	if !ok || isNilInterface(beginner) {
		return callback(e)
	}
	tx, beginErr := beginner.BeginTx(ctx, nil)
	if beginErr != nil {
		return executionError("batch begin", "", beginErr)
	}
	if tx == nil {
		return executionError("batch begin", "", errors.New("beginner returned nil transaction"))
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		rollbackErr := tx.Rollback()
		if rollbackErr == nil || errors.Is(rollbackErr, sql.ErrTxDone) {
			return
		}
		wrapped := executionError("batch rollback", "", rollbackErr)
		if err == nil {
			err = wrapped
		} else {
			err = errors.Join(err, wrapped)
		}
	}()

	child := &Executor{db: tx, observer: e.observer, now: e.now}
	if callbackErr := callback(child); callbackErr != nil {
		return callbackErr
	}
	if commitErr := tx.Commit(); commitErr != nil {
		return executionError("batch commit", "", commitErr)
	}
	committed = true
	return nil
}
