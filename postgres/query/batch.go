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

// BatchSender is implemented by pgxpool.Pool and pgx.Tx.
type BatchSender interface {
	SendBatch(context.Context, *pgx.Batch) pgx.BatchResults
}

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

// ExecBatch executes every queued non-returning statement and closes all pgx
// batch results on every path.
func ExecBatch(ctx context.Context, executor *Executor, batch Batch) (tags []pgconn.CommandTag, err error) {
	if executor == nil || executor.db == nil {
		return nil, fmt.Errorf("query: batch requires a configured executor")
	}
	if batch.err != nil {
		return nil, batch.err
	}
	if len(batch.entries) == 0 {
		return nil, fmt.Errorf("query: batch requires at least one statement")
	}
	sender, ok := executor.db.(BatchSender)
	if !ok {
		return nil, fmt.Errorf("query: executor database does not support pgx batches")
	}

	pgxBatch := &pgx.Batch{}
	started := make([]time.Time, len(batch.entries))
	for index, entry := range batch.entries {
		if validateErr := executor.validate(entry.operation, entry.statement); validateErr != nil {
			return nil, validateErr
		}
		pgxBatch.Queue(entry.statement.SQL, entry.statement.Args...)
		started[index] = executor.start(ctx, entry.operation, entry.statement.SQL)
	}

	results := sender.SendBatch(ctx, pgxBatch)
	closed := false
	defer func() {
		if closed {
			return
		}
		closeErr := results.Close()
		if closeErr == nil {
			return
		}
		wrapped := executionError("batch close", "", closeErr)
		if err == nil {
			err = wrapped
		} else {
			err = errors.Join(err, wrapped)
		}
	}()

	tags = make([]pgconn.CommandTag, 0, len(batch.entries))
	for index, entry := range batch.entries {
		tag, execErr := results.Exec()
		executor.finish(ctx, entry.operation, entry.statement.SQL, started[index], tag.RowsAffected(), execErr)
		if execErr != nil {
			return nil, executionError(entry.operation, entry.statement.SQL, execErr)
		}
		tags = append(tags, tag)
	}
	if closeErr := results.Close(); closeErr != nil {
		closed = true
		return nil, executionError("batch close", "", closeErr)
	}
	closed = true
	return tags, nil
}

// FetchBatch executes homogeneous typed SELECT builders in one pgx batch.
func FetchBatch[T any](ctx context.Context, executor *Executor, builders ...SelectBuilder[T]) (result [][]T, err error) {
	if executor == nil || executor.db == nil {
		return nil, fmt.Errorf("query: select batch requires a configured executor")
	}
	if len(builders) == 0 {
		return nil, fmt.Errorf("query: select batch requires at least one SELECT")
	}
	sender, ok := executor.db.(BatchSender)
	if !ok {
		return nil, fmt.Errorf("query: executor database does not support pgx batches")
	}

	statements := make([]Statement, len(builders))
	started := make([]time.Time, len(builders))
	pgxBatch := &pgx.Batch{}
	for index, builder := range builders {
		statement, buildErr := builder.Build()
		if buildErr != nil {
			return nil, fmt.Errorf("query: select batch item %d: %w", index, buildErr)
		}
		if validateErr := executor.validate("batch select", statement); validateErr != nil {
			return nil, validateErr
		}
		statements[index] = statement
		pgxBatch.Queue(statement.SQL, statement.Args...)
		started[index] = executor.start(ctx, "batch select", statement.SQL)
	}

	results := sender.SendBatch(ctx, pgxBatch)
	closed := false
	defer func() {
		if closed {
			return
		}
		closeErr := results.Close()
		if closeErr == nil {
			return
		}
		wrapped := executionError("select batch close", "", closeErr)
		if err == nil {
			err = wrapped
		} else {
			err = errors.Join(err, wrapped)
		}
	}()

	result = make([][]T, len(builders))
	for index, builder := range builders {
		rows, queryErr := results.Query()
		if queryErr != nil {
			executor.finish(ctx, "batch select", statements[index].SQL, started[index], 0, queryErr)
			return nil, executionError("batch select", statements[index].SQL, queryErr)
		}
		values := make([]T, 0)
		for rows.Next() {
			value, scanErr := builder.table.Scan(rows)
			if scanErr != nil {
				rows.Close()
				executor.finish(ctx, "batch select", statements[index].SQL, started[index], int64(len(values)), scanErr)
				return nil, executionError("batch select scan", statements[index].SQL, scanErr)
			}
			values = append(values, value)
		}
		rowsErr := rows.Err()
		rows.Close()
		if rowsErr != nil {
			executor.finish(ctx, "batch select", statements[index].SQL, started[index], int64(len(values)), rowsErr)
			return nil, executionError("batch select rows", statements[index].SQL, rowsErr)
		}
		result[index] = values
		executor.finish(ctx, "batch select", statements[index].SQL, started[index], int64(len(values)), nil)
	}
	if closeErr := results.Close(); closeErr != nil {
		closed = true
		return nil, executionError("select batch close", "", closeErr)
	}
	closed = true
	return result, nil
}
