package rcapp

import (
	"context"
	"sync"

	"github.com/sevlumen/orm/postgres/query"
)

// EventRecord is the redacted application-owned observer representation.
type EventRecord struct {
	Operation    string
	SQL          string
	RowsAffected int64
	Failed       bool
}

// Recorder captures observer events without retaining query arguments or raw
// database errors. It is safe for concurrent executor use.
type Recorder struct {
	mu     sync.Mutex
	events []EventRecord
}

func (recorder *Recorder) Before(context.Context, query.Event) {}

func (recorder *Recorder) After(_ context.Context, event query.Event) {
	if recorder == nil {
		return
	}
	recorder.mu.Lock()
	recorder.events = append(recorder.events, EventRecord{
		Operation:    event.Operation,
		SQL:          event.SQL,
		RowsAffected: event.RowsAffected,
		Failed:       event.Err != nil,
	})
	recorder.mu.Unlock()
}

// Events returns a defensive copy of recorded events.
func (recorder *Recorder) Events() []EventRecord {
	if recorder == nil {
		return nil
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return append([]EventRecord(nil), recorder.events...)
}

// Reset clears recorded events.
func (recorder *Recorder) Reset() {
	if recorder == nil {
		return
	}
	recorder.mu.Lock()
	recorder.events = nil
	recorder.mu.Unlock()
}

// NewExecutor creates the application's typed executor with redacted observer hooks.
func NewExecutor(db query.DB, recorder *Recorder) (*query.Executor, error) {
	if recorder == nil {
		return query.NewExecutor(db)
	}
	return query.NewExecutor(db, query.WithObserver(recorder))
}

// CreateAccount inserts and returns one account through generated metadata.
func CreateAccount(ctx context.Context, executor *query.Executor, account Account) (Account, error) {
	assignments := []query.Assignment[Account]{
		AccountORM.ID.Set(account.ID),
		AccountORM.LoginEmail.Set(account.LoginEmail),
		AccountORM.Active.Set(account.Active),
	}
	if account.LegacyNote != nil {
		assignments = append(assignments, AccountORM.LegacyNote.Set(account.LegacyNote))
	}
	if account.DisplayName != nil {
		assignments = append(assignments, AccountORM.DisplayName.Set(account.DisplayName))
	}
	return query.InsertOne(ctx, executor, query.Insert(AccountORM.Table).Row(assignments...))
}

// FindAccountByEmail executes a parameterized optional lookup.
func FindAccountByEmail(ctx context.Context, executor *query.Executor, email string) (Account, bool, error) {
	return query.FetchOptional(ctx, executor, query.Select(AccountORM.Table).
		Where(AccountORM.LoginEmail.Eq(email)).
		OrderBy(AccountORM.ID.Asc()))
}

// SetDisplayName updates and returns exactly one account.
func SetDisplayName(ctx context.Context, executor *query.Executor, id int64, displayName *string) (Account, error) {
	return query.UpdateOne(ctx, executor, query.Update(AccountORM.Table).
		Set(AccountORM.DisplayName.Set(displayName)).
		Where(AccountORM.ID.Eq(id)))
}

// InsertOrdersBatch inserts orders through one explicit atomic batch.
func InsertOrdersBatch(ctx context.Context, executor *query.Executor, orders []Order) error {
	batch := query.NewBatch()
	for _, order := range orders {
		batch = query.QueueInsert(batch, query.Insert(OrderORM.Table).Row(
			OrderORM.ID.Set(order.ID),
			OrderORM.AccountID.Set(order.AccountID),
			OrderORM.Total.Set(order.Total),
			OrderORM.Status.Set(order.Status),
		))
	}
	_, err := query.ExecBatch(ctx, executor, batch)
	return err
}

// AccountOrders returns the explicit relation used by the application.
func AccountOrders() query.ManyRelation[Account, Order, int64] {
	relation, err := query.NewManyRelation(
		"account_orders",
		query.RequiredKey(func(account Account) int64 { return account.ID }),
		query.RequiredKey(func(order Order) int64 { return order.AccountID }),
		query.SelectRelationByColumn(
			query.Select(OrderORM.Table).OrderBy(OrderORM.ID.Asc()),
			OrderORM.AccountID,
		),
		query.WithRelationChunkSize(100),
	)
	if err != nil {
		panic(err)
	}
	return relation
}

// CreateAccountAndOrder commits both mutations atomically.
func CreateAccountAndOrder(ctx context.Context, beginner query.Beginner, recorder *Recorder, account Account, order Order) error {
	options := []query.ExecutorOption{}
	if recorder != nil {
		options = append(options, query.WithObserver(recorder))
	}
	return query.InTransaction(ctx, beginner, query.TxOptions{}, func(executor *query.Executor) error {
		if _, err := CreateAccount(ctx, executor, account); err != nil {
			return err
		}
		_, err := query.InsertOne(ctx, executor, query.Insert(OrderORM.Table).Row(
			OrderORM.ID.Set(order.ID),
			OrderORM.AccountID.Set(order.AccountID),
			OrderORM.Total.Set(order.Total),
			OrderORM.Status.Set(order.Status),
		))
		return err
	}, options...)
}
