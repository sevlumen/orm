package ormcli

import (
	"context"
	"flag"
	"fmt"
)

const rollbackUsage = `Usage: orm rollback [options]

Options:
  --config <path>          Versioned orm JSON config file.
  --database-url <url>     PostgreSQL URL; prefer the configured environment variable.
  --migrations <path>      Migration artifact root directory.
  --history-schema <name>  Migration history schema.
  --history-table <name>   Migration history table.
  --lock-key <int64>       PostgreSQL advisory lock key.
  --timeout <duration>     Positive command timeout.
  --steps <n>              Positive number of migrations to roll back; default 1.
  --yes                    Required rollback confirmation.
  --json                   Emit a versioned JSON result.
`

type rollbackResult struct {
	RolledBack []string `json:"rolledBack"`
	Count      int      `json:"count"`
}

func (app *App) runRollback(ctx context.Context, args []string) error {
	var flags databaseFlags
	var steps int
	var yes bool
	_, err := parseFlags("rollback", rollbackUsage, args, func(set *flag.FlagSet) {
		flags.bind(set, false)
		set.IntVar(&steps, "steps", 1, "number of migrations to roll back")
		set.BoolVar(&yes, "yes", false, "confirm rollback")
	})
	if err != nil {
		return err
	}
	if steps <= 0 {
		return &usageError{message: "--steps must be positive", usage: rollbackUsage}
	}
	if !yes {
		return &usageError{message: "--yes is required for rollback", usage: rollbackUsage}
	}
	resolved, err := app.resolveDatabase(flags)
	if err != nil {
		return &usageError{message: err.Error(), usage: rollbackUsage, cause: err}
	}
	pool, migrationRunner, commandContext, cancel, err := app.openRunner(ctx, resolved)
	if err != nil {
		return err
	}
	defer pool.Close()
	defer cancel()
	rolledBack, err := migrationRunner.Rollback(commandContext, resolved.directory, steps)
	if err != nil {
		return protectError(err, resolved.secrets)
	}
	result := rollbackResult{RolledBack: nonNilStrings(rolledBack), Count: len(rolledBack)}
	human := fmt.Sprintf("rolled back %d migrations\n", len(rolledBack))
	return app.writeResult(flags.jsonOutput, "rollback", result, human)
}
