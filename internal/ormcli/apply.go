package ormcli

import (
	"context"
	"flag"
	"fmt"
)

const applyUsage = `Usage: orm apply [options]

Options:
  --config <path>          Versioned orm JSON config file.
  --database-url <url>     PostgreSQL URL; prefer the configured environment variable.
  --migrations <path>      Migration artifact root directory.
  --history-schema <name>  Migration history schema.
  --history-table <name>   Migration history table.
  --lock-key <int64>       PostgreSQL advisory lock key.
  --max-risk <risk>        safe, review, or destructive.
  --timeout <duration>     Positive command timeout.
  --yes                    Confirm destructive-risk execution.
  --json                   Emit a versioned JSON result.
`

type applyResult struct {
	Applied []string `json:"applied"`
	Count   int      `json:"count"`
}

func (app *App) runApply(ctx context.Context, args []string) error {
	var flags databaseFlags
	var yes bool
	_, err := parseFlags("apply", applyUsage, args, func(set *flag.FlagSet) {
		flags.bind(set, true)
		set.BoolVar(&yes, "yes", false, "confirm destructive execution")
	})
	if err != nil {
		return err
	}
	resolved, err := app.resolveDatabase(flags)
	if err != nil {
		return &usageError{message: err.Error(), usage: applyUsage, cause: err}
	}
	if riskRequiresConfirmation(resolved.runner.MaximumRisk) && !yes {
		return &usageError{message: "--yes is required when --max-risk is destructive", usage: applyUsage}
	}
	pool, migrationRunner, commandContext, cancel, err := app.openRunner(ctx, resolved)
	if err != nil {
		return err
	}
	defer pool.Close()
	defer cancel()
	applied, err := migrationRunner.Apply(commandContext, resolved.directory)
	if err != nil {
		return protectError(err, resolved.secrets)
	}
	result := applyResult{Applied: nonNilStrings(applied), Count: len(applied)}
	human := fmt.Sprintf("applied %d migrations\n", len(applied))
	return app.writeResult(flags.jsonOutput, "apply", result, human)
}
