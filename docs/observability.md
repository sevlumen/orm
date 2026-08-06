# Observability and redaction

Sevlumen ORM exposes observer hooks without requiring a logging or tracing dependency.

## Observer lifecycle

An observer receives `Before` and `After` events for an operation that is actually attempted. Events include:

- operation name;
- parameterized SQL text;
- start time and duration;
- affected-row count when available;
- structured error.

Query arguments are intentionally absent. Do not add them to general application logs.

Observers must be fast, concurrency-safe, and non-blocking relative to the application's latency budget. Observer panics are contained by the executor, but a panic still indicates defective instrumentation and should be fixed.

## Logging example

```go
package data

import (
    "context"
    "log/slog"

    "github.com/sevlumen/orm/postgres/query"
)

type observer struct {
    logger *slog.Logger
}

func (o observer) Before(_ context.Context, event query.Event) {
    o.logger.Debug("database operation started",
        "operation", event.Operation,
        "sql", event.SQL,
    )
}

func (o observer) After(_ context.Context, event query.Event) {
    level := slog.LevelDebug
    if event.Err != nil {
        level = slog.LevelError
    }
    o.logger.Log(context.Background(), level, "database operation completed",
        "operation", event.Operation,
        "sql", event.SQL,
        "duration", event.Duration,
        "rows_affected", event.RowsAffected,
        "error", event.Err,
    )
}
```

Use the request context in a real integration when trace and request identifiers are stored there.

## Tracing

A tracing adapter can start a span in `Before` and end it in `After`, keyed by context and operation lifecycle. Avoid global mutable maps without bounded cleanup. Prefer tracing APIs that attach span state to context or use an instrumentation wrapper around each executor call.

Recommended attributes:

- database system: PostgreSQL;
- operation category;
- parameterized statement when organizational policy allows it;
- duration and result status;
- affected row count;
- migration ID for migration tooling.

Do not attach:

- query arguments;
- database URL or password;
- connection options containing secrets;
- complete PostgreSQL errors known to include sensitive data;
- customer records or identifiers unless explicitly classified and approved.

## Errors

Public execution-error strings contain operation and SQL context but redact the raw driver error text where it could expose values. `errors.Is` and `errors.As` continue to work through `Unwrap`, allowing structured handling without making sensitive detail the default log message.

PostgreSQL server logs can independently include statement text or values depending on server configuration. ORM redaction does not control PostgreSQL logging, proxies, APM agents, or application middleware.

## CLI logs

The CLI redacts:

- the complete database URL;
- decoded password;
- query-encoded password;
- path-encoded password.

Prefer an environment variable over `--database-url`, because process arguments may be visible to operating-system tooling before the CLI can redact them.

The CLI's JSON success output contains no credentials. Stderr retained by CI or deployment systems still requires restricted access and retention policy.

## Metrics

Useful metrics include:

- operation count by stable operation name;
- error count by safe error category;
- duration histogram;
- affected-row histogram where meaningful;
- migration execution duration;
- advisory-lock wait duration measured externally;
- connection-pool metrics from pgx.

Do not use raw SQL or unbounded identifiers as metric labels. Normalize operation names to avoid high-cardinality series.

## Migration observability

Record the release commit, migration ID, checksum, risk, start/end time, and result. Do not record migration SQL in a high-volume metric stream. Retain reviewed artifacts in version control and signed release archives instead.

See [Recovery runbook](recovery.md) for the evidence required after failure.
