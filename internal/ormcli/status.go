package ormcli

import (
	"context"
	"flag"
	"fmt"
)

const statusUsage = `Usage: orm status [options]

Options:
  --config <path>          Versioned orm JSON config file.
  --database-url <url>     PostgreSQL URL; prefer the configured environment variable.
  --migrations <path>      Migration artifact root directory.
  --history-schema <name>  Migration history schema.
  --history-table <name>   Migration history table.
  --lock-key <int64>       PostgreSQL advisory lock key.
  --timeout <duration>     Positive command timeout.
  --json                   Emit a versioned JSON result.
`

type statusResult struct {
	Local   []string `json:"local"`
	Applied []string `json:"applied"`
	Pending []string `json:"pending"`
}

func (app *App) runStatus(ctx context.Context, args []string) error {
	var flags databaseFlags
	_, err := parseFlags("status", statusUsage, args, func(set *flag.FlagSet) {
		flags.bind(set, false)
	})
	if err != nil {
		return err
	}
	resolved, err := app.resolveDatabase(flags)
	if err != nil {
		return &usageError{message: err.Error(), usage: statusUsage, cause: err}
	}
	pool, migrationRunner, commandContext, cancel, err := app.openRunner(ctx, resolved)
	if err != nil {
		return err
	}
	defer pool.Close()
	defer cancel()
	status, err := migrationRunner.Status(commandContext, resolved.directory)
	if err != nil {
		return protectError(err, resolved.secrets)
	}
	result := statusResult{
		Local:   nonNilStrings(status.LocalIDs),
		Applied: nonNilStrings(status.AppliedIDs),
		Pending: nonNilStrings(status.PendingIDs),
	}
	human := fmt.Sprintf("local: %d, applied: %d, pending: %d\n", len(result.Local), len(result.Applied), len(result.Pending))
	return app.writeResult(flags.jsonOutput, "status", result, human)
}

func nonNilStrings(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	return append([]string(nil), values...)
}
