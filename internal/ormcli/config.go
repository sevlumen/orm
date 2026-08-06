package ormcli

import "time"

const (
	configVersion      = 1
	maximumConfigBytes = 1 << 20
	defaultTimeout     = 5 * time.Minute
)

// Config is the versioned JSON configuration consumed by the orm CLI.
type Config struct {
	Version    int             `json:"version"`
	Generate   GenerateConfig  `json:"generate,omitempty"`
	Migrations MigrationConfig `json:"migrations,omitempty"`
}

type GenerateConfig struct {
	Directory string   `json:"directory,omitempty"`
	Output    string   `json:"output,omitempty"`
	Types     []string `json:"types,omitempty"`
}

type MigrationConfig struct {
	Directory     string `json:"directory,omitempty"`
	DatabaseEnv   string `json:"databaseEnv,omitempty"`
	HistorySchema string `json:"historySchema,omitempty"`
	HistoryTable  string `json:"historyTable,omitempty"`
	LockKey       int64  `json:"lockKey,omitempty"`
	MaximumRisk   string `json:"maximumRisk,omitempty"`
	Timeout       string `json:"timeout,omitempty"`
}

func defaultConfig() Config {
	return Config{
		Version: configVersion,
		Generate: GenerateConfig{
			Directory: ".",
			Output:    "orm_gen.go",
		},
		Migrations: MigrationConfig{
			Directory:     "migrations",
			DatabaseEnv:   "DATABASE_URL",
			HistorySchema: "public",
			HistoryTable:  "__sevlumen_migrations",
			MaximumRisk:   "safe",
			Timeout:       defaultTimeout.String(),
		},
	}
}
