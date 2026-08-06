package query

import (
	"fmt"
	"testing"
)

type benchmarkUser struct {
	ID     int64
	Email  string
	Active bool
}

type benchmarkUserColumns struct {
	ID     Column[benchmarkUser, int64]
	Email  Column[benchmarkUser, string]
	Active Column[benchmarkUser, bool]
}

type benchmarkScanner struct {
	id     int64
	email  string
	active bool
}

func (scanner benchmarkScanner) Scan(destinations ...any) error {
	if len(destinations) != 3 {
		return fmt.Errorf("benchmark scanner received %d destinations", len(destinations))
	}
	id, ok := destinations[0].(*int64)
	if !ok {
		return fmt.Errorf("benchmark scanner destination 0 has type %T", destinations[0])
	}
	email, ok := destinations[1].(*string)
	if !ok {
		return fmt.Errorf("benchmark scanner destination 1 has type %T", destinations[1])
	}
	active, ok := destinations[2].(*bool)
	if !ok {
		return fmt.Errorf("benchmark scanner destination 2 has type %T", destinations[2])
	}
	*id, *email, *active = scanner.id, scanner.email, scanner.active
	return nil
}

var (
	benchmarkStatementSink Statement
	benchmarkUserSink      benchmarkUser
	benchmarkErrorSink     error
)

func BenchmarkStatementBuild(b *testing.B) {
	table, columns := benchmarkUserMetadata(b, "benchmark_users")
	builder := Select(table).
		Where(And(columns.ID.Eq(int64(42)), columns.Active.Eq(true))).
		OrderBy(columns.Email.Asc()).
		Limit(1)

	b.Run("Direct", func(b *testing.B) {
		b.ReportAllocs()
		const sql = `SELECT "id", "email", "active" FROM "benchmark_users" WHERE ("id" = $1 AND "active" = $2) ORDER BY "email" ASC LIMIT $3`
		for b.Loop() {
			benchmarkStatementSink = Statement{SQL: sql, Args: []any{int64(42), true, int64(1)}}
		}
	})

	b.Run("Typed", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			benchmarkStatementSink, benchmarkErrorSink = builder.Build()
			if benchmarkErrorSink != nil {
				b.Fatal(benchmarkErrorSink)
			}
		}
	})
}

func BenchmarkRowScan(b *testing.B) {
	table, _ := benchmarkUserMetadata(b, "benchmark_users")
	scanner := benchmarkScanner{id: 42, email: "person@example.com", active: true}

	b.Run("Direct", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			var value benchmarkUser
			if err := scanner.Scan(&value.ID, &value.Email, &value.Active); err != nil {
				b.Fatal(err)
			}
			benchmarkUserSink = value
		}
	})

	b.Run("TypedTable", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			benchmarkUserSink, benchmarkErrorSink = table.Scan(scanner)
			if benchmarkErrorSink != nil {
				b.Fatal(benchmarkErrorSink)
			}
		}
	})
}

func TestTypedHotPathAllocationBudgets(t *testing.T) {
	table, columns := benchmarkUserMetadata(t, "allocation_users")
	builder := Select(table).
		Where(And(columns.ID.Eq(int64(42)), columns.Active.Eq(true))).
		OrderBy(columns.Email.Asc()).
		Limit(1)

	buildAllocations := testing.AllocsPerRun(1000, func() {
		statement, err := builder.Build()
		if err != nil {
			panic(err)
		}
		benchmarkStatementSink = statement
	})
	if buildAllocations > 36 {
		t.Fatalf("typed SELECT build allocations = %.2f, budget = 36", buildAllocations)
	}

	scanner := benchmarkScanner{id: 42, email: "person@example.com", active: true}
	scanAllocations := testing.AllocsPerRun(1000, func() {
		value, err := table.Scan(scanner)
		if err != nil {
			panic(err)
		}
		benchmarkUserSink = value
	})
	if scanAllocations > 2 {
		t.Fatalf("typed table scan allocations = %.2f, budget = 2", scanAllocations)
	}
}

type benchmarkTesting interface {
	Helper()
	Fatal(args ...any)
}

func benchmarkUserMetadata(testing benchmarkTesting, tableName string) (*Table[benchmarkUser], benchmarkUserColumns) {
	testing.Helper()
	table, err := NewTable[benchmarkUser](tableName, []string{"id", "email", "active"}, func(row RowScanner) (benchmarkUser, error) {
		var value benchmarkUser
		err := row.Scan(&value.ID, &value.Email, &value.Active)
		return value, err
	})
	if err != nil {
		testing.Fatal(err)
	}
	id, err := NewColumn[benchmarkUser, int64](table, "id")
	if err != nil {
		testing.Fatal(err)
	}
	email, err := NewColumn[benchmarkUser, string](table, "email")
	if err != nil {
		testing.Fatal(err)
	}
	active, err := NewColumn[benchmarkUser, bool](table, "active")
	if err != nil {
		testing.Fatal(err)
	}
	return table, benchmarkUserColumns{ID: id, Email: email, Active: active}
}
