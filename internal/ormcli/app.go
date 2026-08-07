package ormcli

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/sevlumen/orm/internal/buildinfo"
	postgresdriver "github.com/sevlumen/postgres"
)

const (
	ExitSuccess = 0
	ExitFailure = 1
	ExitUsage   = 2
)

var errUsage = errors.New("command usage error")

// App is the testable ORM command runtime.
type App struct {
	Out          io.Writer
	Err          io.Writer
	LookupEnv    func(string) (string, bool)
	OpenDatabase func(context.Context, string) (*sql.DB, error)
}

// New returns a CLI runtime with production dependencies.
func New() *App {
	return &App{
		Out:          os.Stdout,
		Err:          os.Stderr,
		LookupEnv:    os.LookupEnv,
		OpenDatabase: openPostgresDatabase,
	}
}

func openPostgresDatabase(ctx context.Context, databaseURL string) (*sql.DB, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return postgresdriver.Open(databaseURL)
}

// Run executes one command and returns a stable process exit code.
func (app *App) Run(ctx context.Context, args []string) int {
	if app.Out == nil {
		app.Out = io.Discard
	}
	if app.Err == nil {
		app.Err = io.Discard
	}
	if app.LookupEnv == nil {
		app.LookupEnv = os.LookupEnv
	}
	if app.OpenDatabase == nil {
		app.OpenDatabase = openPostgresDatabase
	}
	if len(args) == 0 {
		app.printRootHelp()
		return ExitUsage
	}
	command := args[0]
	var err error
	switch command {
	case "help", "-h", "--help":
		app.printRootHelp()
		return ExitSuccess
	case "version":
		err = app.runVersion(args[1:])
	case "generate":
		err = app.runGenerate(args[1:])
	case "diff":
		err = app.runDiff(args[1:])
	case "validate":
		err = app.runValidate(args[1:])
	case "status":
		err = app.runStatus(ctx, args[1:])
	case "apply":
		err = app.runApply(ctx, args[1:])
	case "rollback":
		err = app.runRollback(ctx, args[1:])
	default:
		err = fmt.Errorf("%w: unknown command %q", errUsage, command)
	}
	if err == nil {
		return ExitSuccess
	}
	app.writeError(command, err)
	if errors.Is(err, errUsage) {
		return ExitUsage
	}
	return ExitFailure
}

func (app *App) writeError(command string, err error) {
	if strings.TrimSpace(command) == "" {
		command = "orm"
	}
	_, _ = fmt.Fprintf(app.Err, "%s: %v\n", command, err)
}

func (app *App) printRootHelp() {
	_, _ = fmt.Fprintln(app.Out, `Usage: orm <command> [options]

Commands:
  version    print build and source metadata
  generate   generate typed table, column, and scanner metadata
  diff       create one checksummed migration artifact from snapshots
  validate   validate local snapshots and migration artifacts offline
  status     compare local migrations with PostgreSQL history
  apply      apply pending migrations within the configured risk gate
  rollback   roll back the latest applied migrations with confirmation

Run "orm <command> --help" for command-specific options.`)
}

func (app *App) runVersion(args []string) error {
	jsonOutput := false
	for _, arg := range args {
		switch arg {
		case "--json":
			jsonOutput = true
		case "-h", "--help":
			_, _ = fmt.Fprintln(app.Out, "Usage: orm version [--json]")
			return nil
		default:
			return fmt.Errorf("%w: version does not accept %q", errUsage, arg)
		}
	}
	info := buildinfo.Current()
	if jsonOutput {
		encoder := json.NewEncoder(app.Out)
		encoder.SetEscapeHTML(false)
		return encoder.Encode(info)
	}
	_, err := fmt.Fprintf(app.Out, "orm %s\ncommit: %s\nbuild date: %s\ndirty: %t\ngo: %s\nplatform: %s/%s\n", info.Version, info.Commit, info.Date, info.Dirty, info.GoVersion, info.GOOS, info.GOARCH)
	return err
}
