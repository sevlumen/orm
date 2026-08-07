package query

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// IsolationLevel is the driver-neutral transaction isolation level.
type IsolationLevel uint8

const (
	IsolationDefault IsolationLevel = iota
	IsolationReadUncommitted
	IsolationReadCommitted
	IsolationRepeatableRead
	IsolationSerializable
)

// TxOptions configures a transaction without exposing a driver-specific type.
type TxOptions struct {
	Isolation IsolationLevel
	ReadOnly  bool
}

// Beginner is implemented by sql.DB and sql.Conn.
type Beginner interface {
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}

// TransactionError identifies the failed transaction stage without including
// raw driver text in its public Error string. Inspect the cause with errors.As.
type TransactionError struct {
	Stage string
	Err   error
}

func (e *TransactionError) Error() string {
	if e == nil {
		return "query: transaction failed"
	}
	return fmt.Sprintf("query: transaction %s failed", e.Stage)
}

func (e *TransactionError) Unwrap() error { return e.Err }

// InTransaction runs callback with an Executor bound to one database/sql
// transaction. Callback errors and panics trigger rollback. Observer options
// are propagated to the transaction executor.
func InTransaction(
	ctx context.Context,
	beginner Beginner,
	options TxOptions,
	callback func(*Executor) error,
	executorOptions ...ExecutorOption,
) (err error) {
	if isNilInterface(beginner) {
		return fmt.Errorf("query: transaction requires a beginner")
	}
	if callback == nil {
		return fmt.Errorf("query: transaction callback is nil")
	}
	sqlOptions, optionErr := options.sqlOptions()
	if optionErr != nil {
		return optionErr
	}

	tx, beginErr := beginner.BeginTx(ctx, sqlOptions)
	if beginErr != nil {
		return &TransactionError{Stage: "begin", Err: beginErr}
	}
	if tx == nil {
		return &TransactionError{Stage: "begin", Err: errors.New("beginner returned nil transaction")}
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
		wrapped := &TransactionError{Stage: "rollback", Err: rollbackErr}
		if err == nil {
			err = wrapped
		} else {
			err = errors.Join(err, wrapped)
		}
	}()

	executor, executorErr := NewExecutor(tx, executorOptions...)
	if executorErr != nil {
		return &TransactionError{Stage: "executor", Err: executorErr}
	}
	if callbackErr := callback(executor); callbackErr != nil {
		return &TransactionError{Stage: "callback", Err: callbackErr}
	}
	if commitErr := tx.Commit(); commitErr != nil {
		return &TransactionError{Stage: "commit", Err: commitErr}
	}
	committed = true
	return nil
}

func (options TxOptions) sqlOptions() (*sql.TxOptions, error) {
	level := sql.LevelDefault
	switch options.Isolation {
	case IsolationDefault:
	case IsolationReadUncommitted:
		level = sql.LevelReadUncommitted
	case IsolationReadCommitted:
		level = sql.LevelReadCommitted
	case IsolationRepeatableRead:
		level = sql.LevelRepeatableRead
	case IsolationSerializable:
		level = sql.LevelSerializable
	default:
		return nil, fmt.Errorf("query: unsupported transaction isolation level %d", options.Isolation)
	}
	return &sql.TxOptions{Isolation: level, ReadOnly: options.ReadOnly}, nil
}
