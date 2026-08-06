# Benchmarks

The benchmark suite separates pure Go ORM overhead from PostgreSQL network and execution latency. Results should be compared within the same machine, Go version, PostgreSQL version, pool configuration, and commit range.

## Pure Go microbenchmarks

Run statement-rendering and scanner benchmarks without a database:

```text
go test ./postgres/query \
  -run '^$' \
  -bench '^(BenchmarkStatementBuild|BenchmarkRowScan)$' \
  -benchmem \
  -count=5
```

Stable benchmark names:

- `BenchmarkStatementBuild/Direct`
- `BenchmarkStatementBuild/Typed`
- `BenchmarkRowScan/Direct`
- `BenchmarkRowScan/TypedTable`

`BenchmarkStatementBuild` compares construction of the same SQL statement and positional arguments. The direct path creates the SQL/argument pair directly. The typed path renders a preconfigured immutable builder, including predicate validation, identifier quoting, positional placeholder numbering, ordering, and limit rendering.

`BenchmarkRowScan` compares direct destination scanning with `Table.Scan` using the same direct scanner and destination fields. The typed path adds table scanner dispatch but does not use reflection.

Every benchmark calls `ReportAllocs`. `TestTypedHotPathAllocationBudgets` enforces deterministic budgets based on current evidence while retaining a small compiler/runtime margin:

- typed SELECT rendering: at most 36 allocations per build;
- typed table scanning: at most 2 allocations per row.

Do not replace these budgets with nanosecond thresholds in unit tests. Tighten them after repeated measurements and implementation improvements; raise them only with benchmark evidence and an explicit review of the added allocations.

## PostgreSQL benchmarks

Set the same test database variable used by integration tests:

```text
SEVLUMEN_TEST_DATABASE_URL='postgres://postgres:postgres@127.0.0.1:5432/sevlumen_test?sslmode=disable' \
go test ./postgres/query \
  -run '^$' \
  -bench '^(BenchmarkFetchOnePostgreSQL|BenchmarkRelationLoadPostgreSQL)$' \
  -benchmem \
  -count=5
```

Stable benchmark names:

- `BenchmarkFetchOnePostgreSQL/DirectPgx`
- `BenchmarkFetchOnePostgreSQL/TypedFetchOne`
- `BenchmarkRelationLoadPostgreSQL/DirectPgxBatch`
- `BenchmarkRelationLoadPostgreSQL/TypedRelation`

### Fetch-one methodology

Both fetch-one paths use:

- the same `pgxpool.Pool`;
- the same temporary PostgreSQL table and row;
- the same selected columns in the same order;
- the same primary-key predicate;
- the same parameterized `LIMIT 1`;
- direct field-address scanning;
- setup and cleanup outside the timed subbenchmarks.

`DirectPgx` calls `pool.QueryRow(...).Scan(...)`. `TypedFetchOne` calls `FetchOne` with a preconfigured typed builder. Its timing therefore includes immutable builder copying, SQL rendering, executor validation, event lifecycle checks, and typed table scanner dispatch in addition to the same database work.

### Relation methodology

Both relation paths load 100 source rows containing 20 unique account keys and three target rows per key. Both execute one SQL query per operation and report `1 queries/op`.

`DirectPgxBatch` executes a pre-rendered parameterized statement, scans rows directly, groups by key, aligns results with source order, and clones each result slice.

`TypedRelation` performs the same external work through `ManyRelation.Load`, including source-key deduplication, query construction, typed scanning, unexpected-key validation, grouping, source-order alignment, and independent result slices.

This benchmark compares explicit batched relation loading with an optimal direct batched implementation. It is not an N+1 comparison. Query-count correctness is enforced separately by PostgreSQL integration tests.

## Current CI evidence

The first committed benchmark gate ran in GitHub Actions workflow run `31070509264` on August 6, 2026. The pure Go sample used Go `1.25.12`, Linux amd64, and an AMD EPYC 9V74 runner with fixed `100x` iterations:

```text
BenchmarkStatementBuild/Direct-4       468.3 ns/op      48 B/op    1 allocs/op
BenchmarkStatementBuild/Typed-4       4868   ns/op    1064 B/op   32 allocs/op
BenchmarkRowScan/Direct-4              117.2 ns/op      32 B/op    1 allocs/op
BenchmarkRowScan/TypedTable-4          105.8 ns/op      32 B/op    1 allocs/op
```

The PostgreSQL sample used Go `1.25.12`, PostgreSQL `18.4`, Linux amd64, the same CPU family, and fixed `20x` iterations:

```text
BenchmarkFetchOnePostgreSQL/DirectPgx-4             161118 ns/op     954 B/op   13 allocs/op
BenchmarkFetchOnePostgreSQL/TypedFetchOne-4         147032 ns/op    1309 B/op   29 allocs/op
BenchmarkRelationLoadPostgreSQL/DirectPgxBatch-4    239366 ns/op   14716 B/op  293 allocs/op
BenchmarkRelationLoadPostgreSQL/TypedRelation-4     277288 ns/op   29342 B/op  437 allocs/op
```

These are single fixed-iteration smoke samples, not statistically significant latency comparisons. They establish the initial allocation baseline and confirm that benchmark code executes in CI. The fetch-one wall-clock ordering may reverse on another runner because database/protocol variance is larger than the measured difference. The relation sample shows a concrete allocation-optimization opportunity in typed grouping and validation; it is recorded rather than hidden, and future changes should be compared with repeated local samples through `benchstat`.

## CI smoke

CI runs fixed-iteration smoke benchmarks rather than latency-ratio gates:

- pure Go microbenchmarks: `100x` on Go 1.25;
- PostgreSQL benchmarks: `20x` on PostgreSQL 18.

The smoke gate verifies that benchmark code compiles, executes, reports allocations, and retains stable names. Hosted-runner wall-clock results vary with CPU contention, virtualization, database startup, and kernel scheduling, so CI does not fail when typed/direct nanosecond ratios move.

PostgreSQL 14 and 18 integration tests still run under the race detector. PostgreSQL benchmark smoke runs on 18 only to control CI duration; local comparisons may use either supported version when the version is recorded with the result.

## Comparing commits

Capture multiple samples from each commit:

```text
go test ./postgres/query -run '^$' \
  -bench '^(BenchmarkStatementBuild|BenchmarkRowScan)$' \
  -benchmem -count=10 > old.txt

# switch commit or branch

go test ./postgres/query -run '^$' \
  -bench '^(BenchmarkStatementBuild|BenchmarkRowScan)$' \
  -benchmem -count=10 > new.txt
```

Use `benchstat old.txt new.txt` when it is available in the developer environment. `benchstat` is intentionally not a module or runtime dependency of this repository.

For database comparisons, also record:

- Go version and `GOARCH`;
- PostgreSQL version;
- operating system and CPU;
- local or remote database placement;
- pool configuration;
- benchmark count and benchtime;
- whether other workloads shared the database or host.

Do not compare a local direct-pgx run with a hosted typed-runtime run. Network and database variance is normally much larger than builder or scanner overhead.

## Interpreting results

Microbenchmarks isolate code-generation, builder, and scanner costs but do not predict application request latency. PostgreSQL benchmarks include protocol, pool, server execution, and row decoding costs and are closer to production behavior, but still operate on a warm local table with minimal contention.

The primary v1 performance contract is:

- no reflection in generated scanner hot paths;
- explicit, bounded relation query counts;
- immutable builders safe for concurrent reuse;
- allocation regressions caught by deterministic budgets;
- benchmark evidence retained for future comparisons;
- no latency claim based on one hosted-runner sample.
