// Package v1example demonstrates the stable v1 query, transaction, batch,
// relation, observer, and generated-metadata shapes.
package v1example

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sevlumen/orm/postgres/query"
)

// orm:table users
type User struct {
	ID     int64  `orm:"column:id;insertOnly;primaryKey"`
	Email  string `orm:"column:email"`
	Active bool   `orm:"column:active"`
}

// orm:table orders
type Order struct {
	ID     int64 `orm:"column:id;insertOnly;primaryKey"`
	UserID int64 `orm:"column:user_id"`
	Total  int64 `orm:"column:total"`
}

// Metadata mirrors the deterministic output produced by orm/ormgen. Real
// applications should commit generated metadata beside their entity types.
var (
	UserTable  *query.Table[User]
	UserID     query.Column[User, int64]
	UserEmail  query.Column[User, string]
	UserActive query.Column[User, bool]

	OrderTable  *query.Table[Order]
	OrderID     query.Column[Order, int64]
	OrderUserID query.Column[Order, int64]
	OrderTotal  query.Column[Order, int64]
)

func init() {
	UserTable = mustTable("users", []string{"id", "email", "active"}, func(row query.RowScanner) (User, error) {
		var value User
		err := row.Scan(&value.ID, &value.Email, &value.Active)
		return value, err
	})
	UserID = mustColumn[User, int64](UserTable, "id", query.InsertOnlyColumn())
	UserEmail = mustColumn[User, string](UserTable, "email")
	UserActive = mustColumn[User, bool](UserTable, "active")

	OrderTable = mustTable("orders", []string{"id", "user_id", "total"}, func(row query.RowScanner) (Order, error) {
		var value Order
		err := row.Scan(&value.ID, &value.UserID, &value.Total)
		return value, err
	})
	OrderID = mustColumn[Order, int64](OrderTable, "id", query.InsertOnlyColumn())
	OrderUserID = mustColumn[Order, int64](OrderTable, "user_id")
	OrderTotal = mustColumn[Order, int64](OrderTable, "total")
}

// CRUDStatements builds parameterized statements without executing them.
func CRUDStatements(email string) (insert, selectOne, update, deleteOne query.Statement, err error) {
	insert, err = query.Insert(UserTable).
		Row(UserID.Set(1), UserEmail.Set(email), UserActive.Set(true)).
		Returning().
		Build()
	if err != nil {
		return insert, selectOne, update, deleteOne, err
	}
	selectOne, err = query.Select(UserTable).
		Where(UserEmail.Eq(email)).
		OrderBy(UserID.Asc()).
		Limit(1).
		Build()
	if err != nil {
		return insert, selectOne, update, deleteOne, err
	}
	update, err = query.Update(UserTable).
		Set(UserActive.Set(false)).
		Where(UserEmail.Eq(email)).
		Returning().
		Build()
	if err != nil {
		return insert, selectOne, update, deleteOne, err
	}
	deleteOne, err = query.Delete(UserTable).
		Where(UserEmail.Eq(email)).
		Returning().
		Build()
	return insert, selectOne, update, deleteOne, err
}

// InsertUserInTransaction executes one returning insert through an explicit
// transaction. No ambient session or hidden save occurs.
func InsertUserInTransaction(ctx context.Context, beginner query.Beginner, observer query.Observer, user User) error {
	return query.InTransaction(ctx, beginner, pgx.TxOptions{}, func(executor *query.Executor) error {
		_, err := query.InsertOne(ctx, executor, query.Insert(UserTable).
			Row(UserID.Set(user.ID), UserEmail.Set(user.Email), UserActive.Set(user.Active)).
			Returning())
		return err
	}, query.WithObserver(observer))
}

// InsertUsersBatch sends an explicit pgx batch. The caller sees one operation
// and no lazy query is triggered by reading entity fields.
func InsertUsersBatch(ctx context.Context, executor *query.Executor, users []User) error {
	batch := query.NewBatch()
	for _, user := range users {
		batch = query.QueueInsert(batch, query.Insert(UserTable).
			Row(UserID.Set(user.ID), UserEmail.Set(user.Email), UserActive.Set(user.Active)))
	}
	_, err := query.ExecBatch(ctx, executor, batch)
	return err
}

// UserOrders returns an explicit batched relation loader. Source keys are
// deduplicated and chunked; relation access never performs an implicit query.
func UserOrders() query.ManyRelation[User, Order, int64] {
	relation, err := query.NewManyRelation(
		"user_orders",
		query.RequiredKey(func(user User) int64 { return user.ID }),
		query.RequiredKey(func(order Order) int64 { return order.UserID }),
		query.SelectRelationByColumn(query.Select(OrderTable), OrderUserID),
		query.WithRelationChunkSize(500),
	)
	if err != nil {
		panic(err)
	}
	return relation
}

// Observer is a dependency-free adapter point for application logging/tracing.
type Observer struct {
	BeforeEvent func(context.Context, query.Event)
	AfterEvent  func(context.Context, query.Event)
}

func (observer Observer) Before(ctx context.Context, event query.Event) {
	if observer.BeforeEvent != nil {
		observer.BeforeEvent(ctx, event)
	}
}

func (observer Observer) After(ctx context.Context, event query.Event) {
	if observer.AfterEvent != nil {
		observer.AfterEvent(ctx, event)
	}
}

// EventSummary demonstrates safe event fields. Query arguments are not exposed.
func EventSummary(event query.Event) string {
	return fmt.Sprintf("operation=%s duration=%s rows=%d failed=%t", event.Operation, event.Duration.Round(time.Microsecond), event.RowsAffected, event.Err != nil)
}

func mustTable[T any](name string, columns []string, scan query.ScanFunc[T]) *query.Table[T] {
	table, err := query.NewTable(name, columns, scan)
	if err != nil {
		panic(err)
	}
	return table
}

func mustColumn[T, V any](table *query.Table[T], name string, options ...query.ColumnOptions) query.Column[T, V] {
	column, err := query.NewColumn[T, V](table, name, options...)
	if err != nil {
		panic(err)
	}
	return column
}
