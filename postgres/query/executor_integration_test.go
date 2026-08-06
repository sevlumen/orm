package query

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type executorIntegrationColumns struct {
	ID     Column[integrationUser, int64]
	Email  Column[integrationUser, string]
	Active Column[integrationUser, bool]
}

func TestExecutorTransactionsBatchesAndCancellationAgainstPostgreSQL(t *testing.T) {
	connectionString := os.Getenv("SEVLUMEN_TEST_DATABASE_URL")
	if connectionString == "" {
		t.Skip("SEVLUMEN_TEST_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, connectionString)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	tableName := fmt.Sprintf("sl_executor_users_%x", time.Now().UnixNano())
	qualified := pgx.Identifier{tableName}.Sanitize()
	if _, err := pool.Exec(ctx, "CREATE TABLE "+qualified+" (id bigint PRIMARY KEY, email text UNIQUE NOT NULL, active boolean NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, "DROP TABLE IF EXISTS "+qualified)
	}()

	table, columns := executorIntegrationMetadata(t, tableName)
	executor, err := NewExecutor(pool)
	if err != nil {
		t.Fatal(err)
	}

	first, err := InsertOne(ctx, executor, Insert(table).
		Row(columns.ID.Set(1), columns.Email.Set("one@example.com"), columns.Active.Set(true)))
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != 1 || first.Email != "one@example.com" || !first.Active {
		t.Fatalf("unexpected first insert: %#v", first)
	}

	inserted, err := InsertAll(ctx, executor, Insert(table).
		Row(columns.ID.Set(2), columns.Email.Set("two@example.com"), columns.Active.Set(true)).
		Row(columns.ID.Set(3), columns.Email.Set("three@example.com"), columns.Active.Set(true)))
	if err != nil {
		t.Fatal(err)
	}
	if len(inserted) != 2 || inserted[0].ID != 2 || inserted[1].ID != 3 {
		t.Fatalf("unexpected multi-row insert: %#v", inserted)
	}

	all, err := FetchAll(ctx, executor, Select(table).OrderBy(columns.ID.Asc()))
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("fetched rows = %d, want 3", len(all))
	}
	if _, found, err := FetchOptional(ctx, executor, Select(table).Where(columns.ID.Eq(999))); err != nil || found {
		t.Fatalf("optional result found=%v err=%v, want empty", found, err)
	}

	callbackErr := errors.New("force rollback")
	err = InTransaction(ctx, pool, pgx.TxOptions{}, func(transactionExecutor *Executor) error {
		_, insertErr := ExecInsert(ctx, transactionExecutor, Insert(table).
			Row(columns.ID.Set(90), columns.Email.Set("rollback@example.com"), columns.Active.Set(true)))
		if insertErr != nil {
			return insertErr
		}
		return callbackErr
	})
	if !errors.Is(err, callbackErr) {
		t.Fatalf("transaction error = %v, want callback error", err)
	}
	assertExecutorRowAbsent(t, ctx, executor, table, columns, 90)

	panicValue := "transaction panic"
	func() {
		defer func() {
			if recovered := recover(); recovered != panicValue {
				t.Fatalf("recovered panic = %#v, want %#v", recovered, panicValue)
			}
		}()
		_ = InTransaction(ctx, pool, pgx.TxOptions{}, func(transactionExecutor *Executor) error {
			if _, insertErr := ExecInsert(ctx, transactionExecutor, Insert(table).
				Row(columns.ID.Set(91), columns.Email.Set("panic@example.com"), columns.Active.Set(true))); insertErr != nil {
				t.Fatal(insertErr)
			}
			panic(panicValue)
		})
	}()
	assertExecutorRowAbsent(t, ctx, executor, table, columns, 91)

	err = InTransaction(ctx, pool, pgx.TxOptions{}, func(transactionExecutor *Executor) error {
		_, updateErr := UpdateOne(ctx, transactionExecutor, Update(table).
			Set(columns.Active.Set(false)).
			AllRows())
		return updateErr
	})
	if !errors.Is(err, ErrMultipleRows) {
		t.Fatalf("multi-row UpdateOne error = %v, want ErrMultipleRows", err)
	}
	stillActive, err := FetchAll(ctx, executor, Select(table).Where(columns.Active.Eq(true)))
	if err != nil {
		t.Fatal(err)
	}
	if len(stillActive) != 3 {
		t.Fatalf("rollback preserved active rows = %d, want 3", len(stillActive))
	}

	batch := QueueInsert(NewBatch(), Insert(table).
		Row(columns.ID.Set(4), columns.Email.Set("four@example.com"), columns.Active.Set(true)))
	batch = QueueInsert(batch, Insert(table).
		Row(columns.ID.Set(5), columns.Email.Set("five@example.com"), columns.Active.Set(false)))
	tags, err := ExecBatch(ctx, executor, batch)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 2 || tags[0].RowsAffected() != 1 || tags[1].RowsAffected() != 1 {
		t.Fatalf("unexpected batch tags: %#v", tags)
	}

	selectedBatches, err := FetchBatch(ctx, executor,
		Select(table).Where(columns.ID.Eq(4)),
		Select(table).Where(columns.ID.Eq(5)),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(selectedBatches) != 2 || len(selectedBatches[0]) != 1 || len(selectedBatches[1]) != 1 {
		t.Fatalf("unexpected select batch: %#v", selectedBatches)
	}

	failingBatch := QueueInsert(NewBatch(), Insert(table).
		Row(columns.ID.Set(6), columns.Email.Set("six@example.com"), columns.Active.Set(true)))
	failingBatch = QueueInsert(failingBatch, Insert(table).
		Row(columns.ID.Set(6), columns.Email.Set("duplicate@example.com"), columns.Active.Set(true)))
	if _, err := ExecBatch(ctx, executor, failingBatch); err == nil {
		t.Fatal("expected duplicate-key batch error")
	}
	assertExecutorRowAbsent(t, ctx, executor, table, columns, 6)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("pool unusable after failed batch: %v", err)
	}

	cancelCtx, cancelQuery := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancelQuery()
	if _, err := executor.Exec(cancelCtx, "cancellation probe", Statement{SQL: "SELECT pg_sleep(1)"}); err == nil {
		t.Fatal("expected cancellation error")
	}
	if cancelCtx.Err() == nil {
		t.Fatal("cancellation context did not expire")
	}
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("pool unusable after cancellation: %v", err)
	}
}

func executorIntegrationMetadata(t *testing.T, tableName string) (*Table[integrationUser], executorIntegrationColumns) {
	t.Helper()
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
	return table, executorIntegrationColumns{ID: id, Email: email, Active: active}
}

func assertExecutorRowAbsent(
	t *testing.T,
	ctx context.Context,
	executor *Executor,
	table *Table[integrationUser],
	columns executorIntegrationColumns,
	id int64,
) {
	t.Helper()
	_, found, err := FetchOptional(ctx, executor, Select(table).Where(columns.ID.Eq(id)))
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatalf("row %d unexpectedly exists", id)
	}
}
