package ormcli

import (
	"context"
	"flag"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sevlumen/orm/migration"
	"github.com/sevlumen/orm/postgres/runner"
)

type databaseFlags struct {
	configPath         string
	databaseURL        optionalString
	migrationsDirectory optionalString
	historySchema      optionalString
	historyTable       optionalString
	lockKey            optionalInt64
	maximumRisk        optionalString
	timeout            optionalString
	jsonOutput         bool
}

func (flags *databaseFlags) bind(set *flag.FlagSet, includeRisk bool) {
	set.StringVar(&flags.configPath, "config", "", "versioned orm JSON config file")
	set.Var(&flags.databaseURL, "database-url", "PostgreSQL URL; prefer configured environment variable")
	set.Var(&flags.migrationsDirectory, "migrations", "migration artifact root")
	set.Var(&flags.historySchema, "history-schema", "migration history schema")
	set.Var(&flags.historyTable, "history-table", "migration history table")
	set.Var(&flags.lockKey, "lock-key", "PostgreSQL advisory lock key")
	if includeRisk {
		set.Var(&flags.maximumRisk, "max-risk", "maximum migration risk")
	}
	set.Var(&flags.timeout, "timeout", "positive command timeout")
	set.BoolVar(&flags.jsonOutput, "json", false, "emit JSON")
}

type optionalInt64 struct {
	value int64
	set   bool
}

func (value *optionalInt64) String() string {
	return strconv.FormatInt(value.value, 10)
}

func (value *optionalInt64) Set(input string) error {
	parsed, err := strconv.ParseInt(input, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid int64 value %q", input)
	}
	value.value = parsed
	value.set = true
	return nil
}

type resolvedDatabase struct {
	directory string
	url       string
	timeout   time.Duration
	runner    runner.Config
	secrets   []string
}

func (app *App) resolveDatabase(flags databaseFlags) (resolvedDatabase, error) {
	config, err := loadConfig(flags.configPath)
	if err != nil {
		return resolvedDatabase{}, err
	}
	migrationConfig := config.Migrations
	if flags.migrationsDirectory.set {
		migrationConfig.Directory = flags.migrationsDirectory.value
	}
	if flags.historySchema.set {
		migrationConfig.HistorySchema = flags.historySchema.value
	}
	if flags.historyTable.set {
		migrationConfig.HistoryTable = flags.historyTable.value
	}
	if flags.lockKey.set {
		migrationConfig.LockKey = flags.lockKey.value
	}
	if flags.maximumRisk.set {
		migrationConfig.MaximumRisk = flags.maximumRisk.value
	}
	if flags.timeout.set {
		migrationConfig.Timeout = flags.timeout.value
	}
	if err := validateConfig(Config{Version: configVersion, Generate: config.Generate, Migrations: migrationConfig}); err != nil {
		return resolvedDatabase{}, err
	}
	maximumRisk, err := parseRisk(migrationConfig.MaximumRisk)
	if err != nil {
		return resolvedDatabase{}, err
	}
	timeout, err := time.ParseDuration(migrationConfig.Timeout)
	if err != nil || timeout <= 0 {
		return resolvedDatabase{}, fmt.Errorf("timeout must be a positive duration")
	}
	databaseURL := ""
	if flags.databaseURL.set {
		databaseURL = strings.TrimSpace(flags.databaseURL.value)
	} else if value, exists := app.LookupEnv(migrationConfig.DatabaseEnv); exists {
		databaseURL = strings.TrimSpace(value)
	}
	if databaseURL == "" {
		return resolvedDatabase{}, fmt.Errorf("database URL is required through --database-url or environment variable %s", migrationConfig.DatabaseEnv)
	}
	return resolvedDatabase{
		directory: migrationConfig.Directory,
		url:       databaseURL,
		timeout:   timeout,
		runner: runner.Config{
			HistorySchema: migrationConfig.HistorySchema,
			HistoryTable:  migrationConfig.HistoryTable,
			LockKey:       migrationConfig.LockKey,
			MaximumRisk:   maximumRisk,
		},
		secrets: databaseSecrets(databaseURL),
	}, nil
}

func (app *App) openRunner(parent context.Context, resolved resolvedDatabase) (*pgxpool.Pool, *runner.Runner, context.Context, context.CancelFunc, error) {
	ctx, cancel := context.WithTimeout(parent, resolved.timeout)
	pool, err := app.OpenPool(ctx, resolved.url)
	if err != nil {
		cancel()
		return nil, nil, nil, nil, protectError(fmt.Errorf("connect to PostgreSQL: %w", err), resolved.secrets)
	}
	if pool == nil {
		cancel()
		return nil, nil, nil, nil, fmt.Errorf("database connector returned nil pool")
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		cancel()
		return nil, nil, nil, nil, protectError(fmt.Errorf("ping PostgreSQL: %w", err), resolved.secrets)
	}
	migrationRunner, err := runner.New(pool, resolved.runner)
	if err != nil {
		pool.Close()
		cancel()
		return nil, nil, nil, nil, err
	}
	return pool, migrationRunner, ctx, cancel, nil
}

type protectedError struct {
	err     error
	secrets []string
}

func (errorValue *protectedError) Error() string {
	message := errorValue.err.Error()
	for _, secret := range errorValue.secrets {
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "[REDACTED]")
		}
	}
	return message
}

func (errorValue *protectedError) Unwrap() error { return errorValue.err }

func protectError(err error, secrets []string) error {
	if err == nil {
		return nil
	}
	return &protectedError{err: err, secrets: append([]string(nil), secrets...)}
}

func databaseSecrets(databaseURL string) []string {
	secrets := []string{databaseURL}
	parsed, err := url.Parse(databaseURL)
	if err == nil && parsed.User != nil {
		if password, exists := parsed.User.Password(); exists && password != "" {
			secrets = append(secrets, password, url.QueryEscape(password))
		}
	}
	return secrets
}

func riskRequiresConfirmation(risk migration.Risk) bool {
	return risk == migration.RiskDestructive
}
