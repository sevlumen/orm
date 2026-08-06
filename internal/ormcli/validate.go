package ormcli

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/sevlumen/orm/migration/artifact"
)

const validateUsage = `Usage: orm validate [options]

Options:
  --config <path>       Versioned orm JSON config file.
  --snapshot <path>     Validate one snapshot JSON instead of migration artifacts.
  --migrations <path>   Migration artifact root directory.
  --json                Emit a versioned JSON result.
`

type validateResult struct {
	Mode          string `json:"mode"`
	Path          string `json:"path"`
	Artifacts     int    `json:"artifacts"`
	LatestID      string `json:"latestId,omitempty"`
	Safe          int    `json:"safe"`
	Review        int    `json:"review"`
	Destructive   int    `json:"destructive"`
	SnapshotValid bool   `json:"snapshotValid"`
}

func (app *App) runValidate(_ context.Context, args []string) error {
	var configPath string
	var snapshotPath string
	var migrationsDirectory optionalString
	var jsonOutput bool
	_, err := parseFlags("validate", validateUsage, args, func(set *flag.FlagSet) {
		set.StringVar(&configPath, "config", "", "versioned orm JSON config file")
		set.StringVar(&snapshotPath, "snapshot", "", "snapshot JSON")
		set.Var(&migrationsDirectory, "migrations", "migration artifact root")
		set.BoolVar(&jsonOutput, "json", false, "emit JSON")
	})
	if err != nil {
		return err
	}
	config, err := loadConfig(configPath)
	if err != nil {
		return &usageError{message: err.Error(), usage: validateUsage, cause: err}
	}
	if strings.TrimSpace(snapshotPath) != "" {
		if migrationsDirectory.set {
			return &usageError{message: "--snapshot cannot be combined with --migrations", usage: validateUsage}
		}
		if _, err := loadSnapshot(snapshotPath); err != nil {
			return err
		}
		result := validateResult{Mode: "snapshot", Path: snapshotPath, SnapshotValid: true}
		return app.writeResult(jsonOutput, "validate", result, fmt.Sprintf("validated snapshot %s\n", snapshotPath))
	}

	directory := config.Migrations.Directory
	if migrationsDirectory.set {
		directory = migrationsDirectory.value
	}
	ids, err := artifact.List(directory)
	if err != nil {
		return err
	}
	result := validateResult{Mode: "migrations", Path: directory, Artifacts: len(ids), SnapshotValid: true}
	for _, id := range ids {
		loaded, err := artifact.Load(directory, id)
		if err != nil {
			return fmt.Errorf("validate migration %s: %w", id, err)
		}
		switch loaded.Manifest.Risk {
		case "safe":
			result.Safe++
		case "review":
			result.Review++
		case "destructive":
			result.Destructive++
		default:
			return fmt.Errorf("migration %s has unsupported risk %q", id, loaded.Manifest.Risk)
		}
		result.LatestID = id
	}
	human := fmt.Sprintf("validated %d migration artifacts in %s\n", len(ids), directory)
	return app.writeResult(jsonOutput, "validate", result, human)
}
