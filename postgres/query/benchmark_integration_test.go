package query

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"
)

type benchmarkOrder struct {
	ID        int64
	AccountID int64
}

type benchmarkOrderColumns struct {
	ID        Column[benchmarkOrder, int64]
	AccountID Column[benchmarkOrder, int64]
}

var (
	benchmarkUsersSink     []benchmarkUser
	benchmarkRelationsSink [][]benchmarkOrder
)

func BenchmarkFetchOnePostgreSQL(b *testing.B) {
	db, tableName, cleanup := benchmarkDatabase(b, "fetch_one")
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	qualified := quoteIdentifier(tableName)
	if _, err := db.ExecContext(ctx, "CREATE TABLE "+qualified+" (id bigint PRIMARY KEY, email text NOT NULL, active boolean NOT NULL)"); err != nil {
		b.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO "+qualified+" (id, email, active) VALUES (42, 'person@example.com', true)"); err != nil {
		b.Fatal(err)
	}

	table, columns := benchmarkUserMetadata(b, tableName)
	executor, err := NewExecutor(db)
	if err != nil {
		b.Fatal(err)
	}
	builder := Select(table).Where(columns.ID.Eq(int64(42)))
	directSQL := "SELECT \"id\", \"email\", \"active\" FROM " + qualified + " WHERE \"id\" = $1 LIMIT $2"

	b.Run("DirectSevlumenPostgres", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for index := 0; index < b.N; index++ {
			var value benchmarkUser
			if err := db.QueryRowContext(ctx, directSQL, int64(42), int64(1)).Scan(&value.ID, &value.Email, &value.Active); err != nil {
				b.Fatal(err)
			}
			benchmarkUserSink = value
		}
	})

	b.Run("TypedFetchOne", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for index := 0; index < b.N; index++ {
			value, err := FetchOne(ctx, executor, builder)
			if err != nil {
				b.Fatal(err)
			}
			benchmarkUserSink = value
		}
	})
}

func BenchmarkRelationLoadPostgreSQL(b *testing.B) {
	db, tableName, cleanup := benchmarkDatabase(b, "relation")
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	qualified := quoteIdentifier(tableName)
	if _, err := db.ExecContext(ctx, "CREATE TABLE "+qualified+" (id bigint PRIMARY KEY, account_id bigint NOT NULL)"); err != nil {
		b.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO "+qualified+" (id, account_id) SELECT account_id * 10 + item, account_id FROM generate_series(1, 20) AS account_id CROSS JOIN generate_series(1, 3) AS item"); err != nil {
		b.Fatal(err)
	}

	orderTable, orderColumns := benchmarkOrderMetadata(b, tableName)
	executor, err := NewExecutor(db)
	if err != nil {
		b.Fatal(err)
	}
	sources := make([]benchmarkUser, 100)
	uniqueKeys := make([]int64, 20)
	for index := range uniqueKeys {
		uniqueKeys[index] = int64(index + 1)
	}
	for index := range sources {
		sources[index].ID = int64(index%len(uniqueKeys) + 1)
	}
	base := Select(orderTable).OrderBy(orderColumns.ID.Asc())
	loader, err := NewManyRelation(
		"benchmark account orders",
		RequiredKey(func(value benchmarkUser) int64 { return value.ID }),
		RequiredKey(func(value benchmarkOrder) int64 { return value.AccountID }),
		SelectRelationByColumn(base, orderColumns.AccountID),
	)
	if err != nil {
		b.Fatal(err)
	}
	directStatement, err := base.Where(orderColumns.AccountID.In(uniqueKeys...)).Build()
	if err != nil {
		b.Fatal(err)
	}

	b.Run("DirectSevlumenPostgresBatch", func(b *testing.B) {
		b.ReportAllocs()
		b.ReportMetric(1, "queries/op")
		b.ResetTimer()
		for index := 0; index < b.N; index++ {
			rows, err := db.QueryContext(ctx, directStatement.SQL, directStatement.Args...)
			if err != nil {
				b.Fatal(err)
			}
			grouped := make(map[int64][]benchmarkOrder, len(uniqueKeys))
			for rows.Next() {
				var value benchmarkOrder
				if err := rows.Scan(&value.ID, &value.AccountID); err != nil {
					rows.Close()
					b.Fatal(err)
				}
				grouped[value.AccountID] = append(grouped[value.AccountID], value)
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				b.Fatal(err)
			}
			aligned := make([][]benchmarkOrder, len(sources))
			for index, source := range sources {
				aligned[index] = append([]benchmarkOrder(nil), grouped[source.ID]...)
			}
			benchmarkRelationsSink = aligned
		}
	})

	b.Run("TypedRelation", func(b *testing.B) {
		b.ReportAllocs()
		b.ReportMetric(1, "queries/op")
		b.ResetTimer()
		for index := 0; index < b.N; index++ {
			loaded, err := loader.Load(ctx, executor, sources)
			if err != nil {
				b.Fatal(err)
			}
			aligned := make([][]benchmarkOrder, len(loaded))
			for index := range loaded {
				aligned[index] = loaded[index].Values
			}
			benchmarkRelationsSink = aligned
		}
	})
}

func benchmarkDatabase(b *testing.B, prefix string) (*sql.DB, string, func()) {
	b.Helper()
	db := openIntegrationDatabase(b)
	tableName := fmt.Sprintf("sl_benchmark_%s_%x", prefix, time.Now().UnixNano())
	cleanup := func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanupCtx, "DROP TABLE IF EXISTS "+quoteIdentifier(tableName))
	}
	return db, tableName, cleanup
}

func benchmarkOrderMetadata(testing benchmarkTesting, tableName string) (*Table[benchmarkOrder], benchmarkOrderColumns) {
	testing.Helper()
	table, err := NewTable[benchmarkOrder](tableName, []string{"id", "account_id"}, func(row RowScanner) (benchmarkOrder, error) {
		var value benchmarkOrder
		err := row.Scan(&value.ID, &value.AccountID)
		return value, err
	})
	if err != nil {
		testing.Fatal(err)
	}
	id, err := NewColumn[benchmarkOrder, int64](table, "id")
	if err != nil {
		testing.Fatal(err)
	}
	accountID, err := NewColumn[benchmarkOrder, int64](table, "account_id")
	if err != nil {
		testing.Fatal(err)
	}
	return table, benchmarkOrderColumns{ID: id, AccountID: accountID}
}
