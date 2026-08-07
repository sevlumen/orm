package ormcli

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/sevlumen/orm/internal/buildinfo"
	postgres "github.com/sevlumen/postgres"
)

const outputVersion = 1

// App is a testable orm command runtime.
type App struct {
	In           io.Reader
	Out          io.Writer
	Err          io.Writer
	LookupEnv    func(string) (string, bool)
	OpenDatabase func(string) (*sql.DB, error)
	BuildInfo    func() buildinfo.Info
}

// New returns a CLI runtime with process defaults.
func New() *App {
	return &App{
		In:           os.Stdin,
		Out:          os.Stdout,
		Err:          os.Stderr,
		LookupEnv:    os.LookupEnv,
		OpenDatabase: postgres.Open,
		BuildInfo:    buildinfo.Current,
	}
}

// Run executes one command and returns a stable process exit code.
func (app *App) Run(ctx context.Context, args []string) int {
	app.ensureDefaults()
	if len(args) == 0 {
		app.writeError(&usageError{message: "a command is required", usage: rootUsage})
		return 2
	}
	var err error
	switch args[0] {
	case "help", "-h", "--help":
		_, _ = io.WriteString(app.Out, rootUsage)
		return 0
	case "version":
		err = app.runVersion(args[1:])
	case "generate":
		err = app.runGenerate(ctx, args[1:])
	case "diff":
		err = app.runDiff(ctx, args[1:])
	case "validate":
		err = app.runValidate(ctx, args[1:])
	case "status":
		err = app.runStatus(ctx, args[1:])
	case "apply":
		err = app.runApply(ctx, args[1:])
	case "rollback":
		err = app.runRollback(ctx, args[1:])
	default:
		err = &usageError{message: fmt.Sprintf("unknown command %q", args[0]), usage: rootUsage}
	}
	if err == nil {
		return 0
	}
	var help *helpError
	if errors.As(err, &help) {
		_, _ = io.WriteString(app.Out, help.usage)
		return 0
	}
	app.writeError(err)
	var usage *usageError
	if errors.As(err, &usage) {
		return 2
	}
	return 1
}

func (app *App) ensureDefaults() {
	if app.In == nil {
		app.In = strings.NewReader("")
	}
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
		app.OpenDatabase = postgres.Open
	}
	if app.BuildInfo == nil {
		app.BuildInfo = buildinfo.Current
	}
}

func (app *App) writeError(err error) {
	message := strings.TrimSpace(err.Error())
	if message == "" {
		message = "command failed"
	}
	_, _ = fmt.Fprintf(app.Err, "orm: %s\n", message)
	var usage *usageError
	if errors.As(err, &usage) && usage.usage != "" {
		_, _ = io.WriteString(app.Err, usage.usage)
	}
}

func (app *App) writeResult(jsonOutput bool, command string, result any, human string) error {
	if jsonOutput {
		encoder := json.NewEncoder(app.Out)
		encoder.SetEscapeHTML(false)
		return encoder.Encode(outputEnvelope{Version: outputVersion, Command: command, Result: result})
	}
	if human != "" {
		_, err := io.WriteString(app.Out, human)
		return err
	}
	return nil
}

type outputEnvelope struct {
	Version int    `json:"version"`
	Command string `json:"command"`
	Result  any    `json:"result"`
}

type usageError struct {
	message string
	usage   string
	cause   error
}

func (e *usageError) Error() string {
	if e.message != "" {
		return e.message
	}
	if e.cause != nil {
		return e.cause.Error()
	}
	return "invalid command usage"
}

func (e *usageError) Unwrap() error { return e.cause }

type helpError struct{ usage string }

func (e *helpError) Error() string { return "help requested" }

func parseFlags(name, usage string, args []string, configure func(*flag.FlagSet)) (*flag.FlagSet, error) {
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	set.SetOutput(io.Discard)
	configure(set)
	if err := set.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, &helpError{usage: usage}
		}
		return nil, &usageError{message: err.Error(), usage: usage, cause: err}
	}
	if set.NArg() != 0 {
		return nil, &usageError{message: "unexpected positional arguments", usage: usage}
	}
	return set, nil
}

type optionalString struct {
	value string
	set   bool
}

func (value *optionalString) String() string { return value.value }

func (value *optionalString) Set(input string) error {
	value.value = input
	value.set = true
	return nil
}

type stringList []string

func (values *stringList) String() string { return strings.Join(*values, ",") }

func (values *stringList) Set(input string) error {
	if strings.TrimSpace(input) == "" {
		return fmt.Errorf("value cannot be empty")
	}
	*values = append(*values, input)
	return nil
}

const rootUsage = `Usage: orm <command> [options]

Commands:
  version    Print release version and build provenance metadata.
  generate   Generate typed table, column, and scanner metadata.
  diff       Create a checksummed migration artifact from snapshots.
  validate   Validate a snapshot or every local migration artifact.
  status     Compare local artifacts with applied PostgreSQL history.
  apply      Apply pending migrations transactionally.
  rollback   Roll back applied migrations transactionally.

Run "orm <command> --help" for command-specific options.
`
