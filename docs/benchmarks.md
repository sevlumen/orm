# Benchmarks

The benchmark suite separates pure Go ORM overhead from PostgreSQL network and execution latency. Compare results only within the same machine, Go version, PostgreSQL version, database configuration, and commit range.

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

`BenchmarkStatementBuild` compares construction of the same SQL statement and positional arguments. The direct path creates the SQL and argument pair directly. The typed path renders a preconfigured immutable builder, including predicate validation, identifier quoting, placeholder numbering, ordering, and limit rendering.

`BenchmarkRowScan` compares direct destination scanning with `Table.Scan` using the same destination fields. The typed path adds table-scanner dispatch but does not use reflection.

Every benchmark calls `ReportAllocs`. `TestTypedHotPathAllocationBudgets` runs in a dedicated non-race process on both supported Go toolchains and enforces reviewed allocation budgets:

- typed SELECT rendering: at most 36 allocations per build;
- typed table scanning: at most 4 allocations per row.

Do not replace these budgets with nanosecond thresholds in unit tests. Tighten them after repeated measurements and implementation improvements; raise them only with benchmark evidence and explicit review of the added allocations.

## PostgreSQL benchmarks

Set the same database variable used by integration tests:

```text
SEVLUMEN_TEST_DATABASE_URL='postgres://postgres:postgres@127.0.0.1:5432/sevlumen_test?sslmode=disable' \
go test ./postgres/query \
  -run '^$' \
  -bench '^(BenchmarkFetchOnePostgreSQL|BenchmarkRelationLoadPostgreSQL)$' \
  -benchmem \
  -count=5
```

Stable benchmark names:

- `BenchmarkFetchOnePostgreSQL/DirectSevlumenPostgres`
- `BenchmarkFetchOnePostgreSQL/TypedFetchOne`
- `BenchmarkRelationLoadPostgreSQL/DirectSevlumenPostgresBatch`
- `BenchmarkRelationLoadPostgreSQL/TypedRelation`

### Fetch-one methodology

Both fetch-one paths use:

- the same `*sql.DB` backed by `github.com/sevlumen/postgres`;
- the same temporary PostgreSQL table and row;
- the same selected columns in the same order;
- the same primary-key predicate;
- the same parameterized `LIMIT 1`;
- direct field-address scanning;
- setup and cleanup outside the timed subbenchmarks.

`DirectSevlumenPostgres` calls `QueryRowContext(...).Scan(...)`. `TypedFetchOne` calls `FetchOne` with a preconfigured typed builder. Typed timing therefore includes immutable builder copying, SQL rendering, executor validation, event lifecycle checks, and typed table-scanner dispatch in addition to the same database work.

### Relation methodology

Both relation paths load 100 source rows containing 20 unique account keys and three target rows per key. Both execute one SQL query per operation and report `1 queries/op`.

`DirectSevlumenPostgresBatch` executes a pre-rendered parameterized statement, scans rows directly, groups by key, aligns results with source order, and clones each result slice.

`TypedRelation` performs the same external work through `ManyRelation.Load`, including source-key deduplication, query construction, typed scanning, unexpected-key validation, grouping, source-order alignment, and independent result slices.

This compares explicit batched relation loading with an optimal direct batched implementation. It is not an N+1 comparison. Query-count correctness is enforced separately by PostgreSQL integration and release-candidate tests.

## CI evidence

CI runs fixed-iteration smoke benchmarks instead of latency-ratio gates:

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

Use `benchstat old.txt new.txt` when available in the developer environment. `benchstat` is intentionally not a module or runtime dependency.

For database comparisons, also record:

- Go version and `GOARCH`;
- PostgreSQL version;
- operating system and CPU;
- local or remote database placement;
- pool configuration;
- benchmark count and benchtime;
- whether other workloads shared the database or host.

Do not compare runs from different machines or database placements. Network and database variance is normally much larger than builder or scanner overhead.

## Interpreting results

Microbenchmarks isolate code-generation, builder, and scanner costs but do not predict application request latency. PostgreSQL benchmarks include protocol, pool, server execution, and row-decoding costs and are closer to production behavior, but still operate on a warm local table with minimal contention.

The primary v1 performance contract is:

- no reflection in generated scanner hot paths;
- explicit, bounded relation query counts;
- immutable builders safe for concurrent reuse;
- allocation regressions caught by deterministic budgets;
- benchmark evidence retained for future comparisons;
- no latency claim based on one hosted-runner sample.
