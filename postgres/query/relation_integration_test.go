package query

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type relationAccount struct {
	ID        int64
	ManagerID *int64
	TenantID  int64
}

type relationOrder struct {
	ID        int64
	AccountID int64
	TenantID  int64
	Active    bool
}

type relationProfile struct {
	ID        int64
	AccountID int64
	Label     string
}

type relationSetting struct {
	TenantID  int64
	AccountID int64
	Value     string
}

type relationPair struct {
	TenantID  int64
	AccountID int64
}

type relationMetadata struct {
	accounts *Table[relationAccount]
	account  struct {
		ID        Column[relationAccount, int64]
		ManagerID Column[relationAccount, *int64]
		TenantID  Column[relationAccount, int64]
	}
	orders *Table[relationOrder]
	order  struct {
		ID        Column[relationOrder, int64]
		AccountID Column[relationOrder, int64]
		TenantID  Column[relationOrder, int64]
		Active    Column[relationOrder, bool]
	}
	profiles *Table[relationProfile]
	profile  struct {
		ID        Column[relationProfile, int64]
		AccountID Column[relationProfile, int64]
		Label     Column[relationProfile, string]
	}
	settings *Table[relationSetting]
	setting  struct {
		TenantID  Column[relationSetting, int64]
		AccountID Column[relationSetting, int64]
		Value     Column[relationSetting, string]
	}
}

type relationQueryObserver struct {
	selects atomic.Int64
}

func (observer *relationQueryObserver) Before(_ context.Context, event Event) {
	if event.Operation == "select all" {
		observer.selects.Add(1)
	}
}

func (*relationQueryObserver) After(context.Context, Event) {}

func TestTypedRelationsAgainstPostgreSQL(t *testing.T) {
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

	suffix := fmt.Sprintf("%x", time.Now().UnixNano())
	names := map[string]string{
		"accounts": "sl_relation_accounts_" + suffix,
		"orders":   "sl_relation_orders_" + suffix,
		"profiles": "sl_relation_profiles_" + suffix,
		"settings": "sl_relation_settings_" + suffix,
	}
	createRelationTables(t, ctx, pool, names)
	defer dropRelationTables(pool, names)
	seedRelationTables(t, ctx, pool, names)

	metadata := newRelationMetadata(t, names)
	observer := &relationQueryObserver{}
	executor, err := NewExecutor(pool, WithObserver(observer))
	if err != nil {
		t.Fatal(err)
	}

	many, err := NewManyRelation(
		"account orders",
		RequiredKey(func(value relationAccount) int64 { return value.ID }),
		RequiredKey(func(value relationOrder) int64 { return value.AccountID }),
		SelectRelationByColumn(
			Select(metadata.orders).
				Where(metadata.order.Active.Eq(true)).
				OrderBy(metadata.order.ID.Asc()),
			metadata.order.AccountID,
		),
	)
	if err != nil {
		t.Fatal(err)
	}

	sources := []relationAccount{{ID: 1}, {ID: 2}, {ID: 1}, {ID: 999}}
	manyResults, err := many.Load(ctx, executor, sources)
	if err != nil {
		t.Fatal(err)
	}
	if observer.selects.Load() != 1 {
		t.Fatalf("many relation queries = %d, want 1", observer.selects.Load())
	}
	if len(manyResults) != 4 || len(manyResults[0].Values) != 2 || len(manyResults[1].Values) != 1 || len(manyResults[2].Values) != 2 {
		t.Fatalf("unexpected many relation results: %#v", manyResults)
	}
	if manyResults[0].Values[0].ID != 10 || manyResults[0].Values[1].ID != 11 || manyResults[1].Values[0].ID != 20 {
		t.Fatalf("relation order/filter was not preserved: %#v", manyResults)
	}
	if !manyResults[3].KeyPresent || manyResults[3].Values == nil || len(manyResults[3].Values) != 0 {
		t.Fatalf("missing target was not represented explicitly: %#v", manyResults[3])
	}
	manyResults[0].Values[0].ID = 9999
	if manyResults[2].Values[0].ID != 10 {
		t.Fatal("duplicate source keys share a mutable result slice")
	}

	observer.selects.Store(0)
	one, err := NewOneRelation(
		"account manager",
		func(value relationAccount) (int64, bool) {
			if value.ManagerID == nil {
				return 0, false
			}
			return *value.ManagerID, true
		},
		RequiredKey(func(value relationAccount) int64 { return value.ID }),
		SelectRelationByColumn(Select(metadata.accounts), metadata.account.ID),
	)
	if err != nil {
		t.Fatal(err)
	}
	managerOne, missingManager := int64(1), int64(999)
	managerResults, err := one.Load(ctx, executor, []relationAccount{
		{ID: 10, ManagerID: &managerOne},
		{ID: 11},
		{ID: 12, ManagerID: &missingManager},
		{ID: 13, ManagerID: &managerOne},
	})
	if err != nil {
		t.Fatal(err)
	}
	if observer.selects.Load() != 1 {
		t.Fatalf("one relation queries = %d, want 1", observer.selects.Load())
	}
	if !managerResults[0].KeyPresent || !managerResults[0].Found || managerResults[0].Value.ID != 1 {
		t.Fatalf("loaded manager = %#v", managerResults[0])
	}
	if managerResults[1].KeyPresent || managerResults[1].Found {
		t.Fatalf("nullable manager = %#v", managerResults[1])
	}
	if !managerResults[2].KeyPresent || managerResults[2].Found {
		t.Fatalf("missing manager = %#v", managerResults[2])
	}
	if !managerResults[3].Found || managerResults[3].Value.ID != 1 {
		t.Fatalf("duplicate manager source = %#v", managerResults[3])
	}

	observer.selects.Store(0)
	chunked, err := NewManyRelation(
		"chunked orders",
		RequiredKey(func(value relationAccount) int64 { return value.ID }),
		RequiredKey(func(value relationOrder) int64 { return value.AccountID }),
		SelectRelationByColumn(Select(metadata.orders), metadata.order.AccountID),
		WithRelationChunkSize(1),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := chunked.Load(ctx, executor, sources); err != nil {
		t.Fatal(err)
	}
	if observer.selects.Load() != 3 {
		t.Fatalf("chunked relation queries = %d, want 3 unique-key chunks", observer.selects.Load())
	}

	observer.selects.Store(0)
	composite, err := NewOneRelation(
		"tenant account setting",
		RequiredKey(func(value relationAccount) relationPair {
			return relationPair{TenantID: value.TenantID, AccountID: value.ID}
		}),
		RequiredKey(func(value relationSetting) relationPair {
			return relationPair{TenantID: value.TenantID, AccountID: value.AccountID}
		}),
		func(keys []relationPair) SelectBuilder[relationSetting] {
			predicates := make([]Predicate[relationSetting], len(keys))
			for index, key := range keys {
				predicates[index] = And(
					metadata.setting.TenantID.Eq(key.TenantID),
					metadata.setting.AccountID.Eq(key.AccountID),
				)
			}
			return Select(metadata.settings).
				Where(Or(predicates...)).
				OrderBy(metadata.setting.AccountID.Asc())
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	compositeResults, err := composite.Load(ctx, executor, []relationAccount{
		{ID: 1, TenantID: 100},
		{ID: 2, TenantID: 100},
		{ID: 999, TenantID: 100},
	})
	if err != nil {
		t.Fatal(err)
	}
	if observer.selects.Load() != 1 || !compositeResults[0].Found || compositeResults[0].Value.Value != "one" || !compositeResults[1].Found || compositeResults[2].Found {
		t.Fatalf("unexpected composite relation results: %#v, queries=%d", compositeResults, observer.selects.Load())
	}

	duplicate, err := NewOneRelation(
		"account profile",
		RequiredKey(func(value relationAccount) int64 { return value.ID }),
		RequiredKey(func(value relationProfile) int64 { return value.AccountID }),
		SelectRelationByColumn(
			Select(metadata.profiles).OrderBy(metadata.profile.ID.Asc()),
			metadata.profile.AccountID,
		),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := duplicate.Load(ctx, executor, []relationAccount{{ID: 1}}); !errors.Is(err, ErrRelationMultipleRows) {
		t.Fatalf("duplicate one relation error = %v, want ErrRelationMultipleRows", err)
	}

	unexpected, err := NewOneRelation(
		"unexpected setting",
		RequiredKey(func(value relationAccount) relationPair {
			return relationPair{TenantID: value.TenantID, AccountID: value.ID}
		}),
		RequiredKey(func(value relationSetting) relationPair {
			return relationPair{TenantID: value.TenantID, AccountID: value.AccountID}
		}),
		func([]relationPair) SelectBuilder[relationSetting] { return Select(metadata.settings) },
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := unexpected.Load(ctx, executor, []relationAccount{{ID: 999, TenantID: 100}}); !errors.Is(err, ErrRelationUnexpectedKey) {
		t.Fatalf("unexpected-key error = %v, want ErrRelationUnexpectedKey", err)
	}

	slow, err := NewManyRelation(
		"slow orders",
		RequiredKey(func(value relationAccount) int64 { return value.ID }),
		RequiredKey(func(value relationOrder) int64 { return value.AccountID }),
		func(keys []int64) SelectBuilder[relationOrder] {
			return Select(metadata.orders).Where(And(
				metadata.order.AccountID.In(keys...),
				TrustedPredicate(metadata.orders, TrustedSQL("EXISTS (SELECT 1 FROM pg_sleep(0.2))")),
			))
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	cancelCtx, cancelLoad := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancelLoad()
	if _, err := slow.Load(cancelCtx, executor, []relationAccount{{ID: 1}}); err == nil {
		t.Fatal("expected relation cancellation error")
	}
	if cancelCtx.Err() == nil {
		t.Fatal("relation cancellation context did not expire")
	}
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("pool unusable after relation cancellation: %v", err)
	}

	observer.selects.Store(0)
	const workers = 16
	var wait sync.WaitGroup
	errorsCh := make(chan error, workers)
	for index := 0; index < workers; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			loaded, loadErr := many.Load(ctx, executor, sources[:2])
			if loadErr != nil {
				errorsCh <- loadErr
				return
			}
			if len(loaded) != 2 || len(loaded[0].Values) != 2 || len(loaded[1].Values) != 1 {
				errorsCh <- fmt.Errorf("unexpected concurrent relation result")
			}
		}()
	}
	wait.Wait()
	close(errorsCh)
	for loadErr := range errorsCh {
		t.Error(loadErr)
	}
	if observer.selects.Load() != workers {
		t.Fatalf("concurrent relation queries = %d, want %d", observer.selects.Load(), workers)
	}
}

func createRelationTables(t *testing.T, ctx context.Context, pool *pgxpool.Pool, names map[string]string) {
	t.Helper()
	statements := []string{
		"CREATE TABLE " + pgx.Identifier{names["accounts"]}.Sanitize() + " (id bigint PRIMARY KEY, manager_id bigint, tenant_id bigint NOT NULL)",
		"CREATE TABLE " + pgx.Identifier{names["orders"]}.Sanitize() + " (id bigint PRIMARY KEY, account_id bigint NOT NULL, tenant_id bigint NOT NULL, active boolean NOT NULL)",
		"CREATE TABLE " + pgx.Identifier{names["profiles"]}.Sanitize() + " (id bigint PRIMARY KEY, account_id bigint NOT NULL, label text NOT NULL)",
		"CREATE TABLE " + pgx.Identifier{names["settings"]}.Sanitize() + " (tenant_id bigint NOT NULL, account_id bigint NOT NULL, value text NOT NULL)",
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
}

func seedRelationTables(t *testing.T, ctx context.Context, pool *pgxpool.Pool, names map[string]string) {
	t.Helper()
	statements := []string{
		"INSERT INTO " + pgx.Identifier{names["accounts"]}.Sanitize() + " (id, manager_id, tenant_id) VALUES (1, NULL, 100), (2, 1, 100)",
		"INSERT INTO " + pgx.Identifier{names["orders"]}.Sanitize() + " (id, account_id, tenant_id, active) VALUES (10, 1, 100, true), (11, 1, 100, true), (12, 1, 100, false), (20, 2, 100, true)",
		"INSERT INTO " + pgx.Identifier{names["profiles"]}.Sanitize() + " (id, account_id, label) VALUES (100, 1, 'first'), (101, 1, 'duplicate'), (200, 2, 'second')",
		"INSERT INTO " + pgx.Identifier{names["settings"]}.Sanitize() + " (tenant_id, account_id, value) VALUES (100, 1, 'one'), (100, 2, 'two')",
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
}

func dropRelationTables(pool *pgxpool.Pool, names map[string]string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, key := range []string{"settings", "profiles", "orders", "accounts"} {
		_, _ = pool.Exec(ctx, "DROP TABLE IF EXISTS "+pgx.Identifier{names[key]}.Sanitize())
	}
}

func newRelationMetadata(t *testing.T, names map[string]string) relationMetadata {
	t.Helper()
	var metadata relationMetadata
	metadata.accounts = mustRelationTable(t, NewTable[relationAccount](names["accounts"], []string{"id", "manager_id", "tenant_id"}, func(row RowScanner) (relationAccount, error) {
		var value relationAccount
		err := row.Scan(&value.ID, &value.ManagerID, &value.TenantID)
		return value, err
	}))
	metadata.account.ID = mustRelationColumn(t, NewColumn[relationAccount, int64](metadata.accounts, "id"))
	metadata.account.ManagerID = mustRelationColumn(t, NewColumn[relationAccount, *int64](metadata.accounts, "manager_id"))
	metadata.account.TenantID = mustRelationColumn(t, NewColumn[relationAccount, int64](metadata.accounts, "tenant_id"))

	metadata.orders = mustRelationTable(t, NewTable[relationOrder](names["orders"], []string{"id", "account_id", "tenant_id", "active"}, func(row RowScanner) (relationOrder, error) {
		var value relationOrder
		err := row.Scan(&value.ID, &value.AccountID, &value.TenantID, &value.Active)
		return value, err
	}))
	metadata.order.ID = mustRelationColumn(t, NewColumn[relationOrder, int64](metadata.orders, "id"))
	metadata.order.AccountID = mustRelationColumn(t, NewColumn[relationOrder, int64](metadata.orders, "account_id"))
	metadata.order.TenantID = mustRelationColumn(t, NewColumn[relationOrder, int64](metadata.orders, "tenant_id"))
	metadata.order.Active = mustRelationColumn(t, NewColumn[relationOrder, bool](metadata.orders, "active"))

	metadata.profiles = mustRelationTable(t, NewTable[relationProfile](names["profiles"], []string{"id", "account_id", "label"}, func(row RowScanner) (relationProfile, error) {
		var value relationProfile
		err := row.Scan(&value.ID, &value.AccountID, &value.Label)
		return value, err
	}))
	metadata.profile.ID = mustRelationColumn(t, NewColumn[relationProfile, int64](metadata.profiles, "id"))
	metadata.profile.AccountID = mustRelationColumn(t, NewColumn[relationProfile, int64](metadata.profiles, "account_id"))
	metadata.profile.Label = mustRelationColumn(t, NewColumn[relationProfile, string](metadata.profiles, "label"))

	metadata.settings = mustRelationTable(t, NewTable[relationSetting](names["settings"], []string{"tenant_id", "account_id", "value"}, func(row RowScanner) (relationSetting, error) {
		var value relationSetting
		err := row.Scan(&value.TenantID, &value.AccountID, &value.Value)
		return value, err
	}))
	metadata.setting.TenantID = mustRelationColumn(t, NewColumn[relationSetting, int64](metadata.settings, "tenant_id"))
	metadata.setting.AccountID = mustRelationColumn(t, NewColumn[relationSetting, int64](metadata.settings, "account_id"))
	metadata.setting.Value = mustRelationColumn(t, NewColumn[relationSetting, string](metadata.settings, "value"))
	return metadata
}

func mustRelationTable[T any](t *testing.T, table *Table[T], err error) *Table[T] {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	return table
}

func mustRelationColumn[T any, V any](t *testing.T, column Column[T, V], err error) Column[T, V] {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	return column
}
