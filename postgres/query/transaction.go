package query

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

const rollbackTimeout = 5 * time.Second

// Beginner is implemented by pgxpool.Pool and pgx.Tx.
type Beginner interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

// TransactionError identifies the failed transaction stage.
type TransactionError struct {
	Stage string
	Err   error
}

func (e *TransactionError) Error() string {
	return fmt.Sprintf("query: transaction %s failed: %v", e.Stage, e.Err)
}

func (e *TransactionError) Unwrap() error { return e.Err }

// InTransaction runs callback with an Executor bound to one pgx transaction.
// Callback errors and panics trigger rollback. Panics are rethrown after the
// rollback attempt. Observer options are propagated to the transaction executor.
func InTransaction(
	ctx context.Context,
	beginner Beginner,
	options pgx.TxOptions,
	callback func(*Executor) error,
	executorOptions ...ExecutorOption,
) (err error) {
	if isNilInterface(beginner) {
		return fmt.Errorf("query: transaction requires a beginner")
	}
	if callback == nil {
		return fmt.Errorf("query: transaction callback is nil")
	}

	tx, beginErr := beginner.BeginTx(ctx, options)
	if beginErr != nil {
		return &TransactionError{Stage: "begin", Err: beginErr}
	}
	if isNilInterface(tx) {
		return &TransactionError{Stage: "begin", Err: fmt.Errorf("beginner returned nil transaction")}
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		rollbackCtx, cancel := context.WithTimeout(context.Background(), rollbackTimeout)
		rollbackErr := tx.Rollback(rollbackCtx)
		cancel()
		if rollbackErr == nil || errors.Is(rollbackErr, pgx.ErrTxClosed) {
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
	if commitErr := tx.Commit(ctx); commitErr != nil {
		return &TransactionError{Stage: "commit", Err: commitErr}
	}
	committed = true
	return nil
}
