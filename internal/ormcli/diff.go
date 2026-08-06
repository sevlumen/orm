package ormcli

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/sevlumen/orm/migration"
	"github.com/sevlumen/orm/migration/artifact"
	"github.com/sevlumen/orm/postgres"
)

const diffUsage = `Usage: orm diff [options]

Options:
  --config <path>       Versioned orm JSON config file.
  --after <path>        Required target snapshot JSON.
  --before <path>       Optional source snapshot; defaults to latest local artifact.
  --id <id>             Required migration artifact ID.
  --renames <path>      Optional versioned explicit rename-intent JSON.
  --migrations <path>   Migration artifact root directory.
  --max-risk <risk>     safe, review, or destructive.
  --json                Emit a versioned JSON result.
`

type diffResult struct {
	ID         string   `json:"id"`
	Path       string   `json:"path"`
	Risk       string   `json:"risk"`
	Operations int      `json:"operations"`
	Warnings   []string `json:"warnings"`
	PreviousID string   `json:"previousId,omitempty"`
}

func (app *App) runDiff(_ context.Context, args []string) error {
	var configPath string
	var beforePath string
	var afterPath string
	var id string
	var renamePath string
	var migrationsDirectory optionalString
	var maximumRisk optionalString
	var jsonOutput bool
	_, err := parseFlags("diff", diffUsage, args, func(set *flag.FlagSet) {
		set.StringVar(&configPath, "config", "", "versioned orm JSON config file")
		set.StringVar(&beforePath, "before", "", "source snapshot JSON")
		set.StringVar(&afterPath, "after", "", "target snapshot JSON")
		set.StringVar(&id, "id", "", "migration artifact ID")
		set.StringVar(&renamePath, "renames", "", "rename intent JSON")
		set.Var(&migrationsDirectory, "migrations", "migration artifact root")
		set.Var(&maximumRisk, "max-risk", "maximum generated migration risk")
		set.BoolVar(&jsonOutput, "json", false, "emit JSON")
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(afterPath) == "" {
		return &usageError{message: "--after is required", usage: diffUsage}
	}
	if strings.TrimSpace(id) == "" {
		return &usageError{message: "--id is required", usage: diffUsage}
	}
	config, err := loadConfig(configPath)
	if err != nil {
		return &usageError{message: err.Error(), usage: diffUsage, cause: err}
	}
	directory := config.Migrations.Directory
	if migrationsDirectory.set {
		directory = migrationsDirectory.value
	}
	riskName := config.Migrations.MaximumRisk
	if maximumRisk.set {
		riskName = maximumRisk.value
	}
	allowedRisk, err := parseRisk(riskName)
	if err != nil {
		return &usageError{message: err.Error(), usage: diffUsage, cause: err}
	}

	latest, previousID, err := latestLocalSnapshot(directory)
	if err != nil {
		return err
	}
	before := latest
	if strings.TrimSpace(beforePath) != "" {
		before, err = loadSnapshot(beforePath)
		if err != nil {
			return err
		}
		if previousID != "" {
			matches, err := snapshotsEqual(before, latest)
			if err != nil {
				return err
			}
			if !matches {
				return fmt.Errorf("explicit --before snapshot does not match latest local migration %s", previousID)
			}
		}
	}
	if previousID != "" && strings.TrimSpace(id) <= previousID {
		return fmt.Errorf("migration id %q must sort after latest local id %q", id, previousID)
	}
	after, err := loadSnapshot(afterPath)
	if err != nil {
		return err
	}
	options, err := loadRenameOptions(renamePath)
	if err != nil {
		return err
	}
	plan, err := migration.DiffWithOptions(before, after, options)
	if err != nil {
		return fmt.Errorf("plan migration: %w", err)
	}
	if len(plan.Operations) == 0 {
		return fmt.Errorf("migration has no schema changes")
	}
	if plan.MaxRisk() > allowedRisk {
		return fmt.Errorf("migration risk %s exceeds allowed maximum %s", plan.MaxRisk(), allowedRisk)
	}
	rendered, err := postgres.RenderMigration(plan)
	if err != nil {
		return fmt.Errorf("render migration: %w", err)
	}
	built, err := artifact.Build(id, rendered, after)
	if err != nil {
		return err
	}
	if err := artifact.Write(directory, built); err != nil {
		return err
	}
	warnings := append([]string(nil), rendered.Warnings...)
	if warnings == nil {
		warnings = []string{}
	}
	result := diffResult{
		ID:         id,
		Path:       filepath.Join(directory, id),
		Risk:       rendered.Risk.String(),
		Operations: len(plan.Operations),
		Warnings:   warnings,
		PreviousID: previousID,
	}
	human := fmt.Sprintf("created migration %s (%s, %d operations) at %s\n", id, rendered.Risk, len(plan.Operations), result.Path)
	return app.writeResult(jsonOutput, "diff", result, human)
}

func snapshotsEqual(left, right migration.Snapshot) (bool, error) {
	leftData, err := left.Marshal()
	if err != nil {
		return false, err
	}
	rightData, err := right.Marshal()
	if err != nil {
		return false, err
	}
	return bytes.Equal(leftData, rightData), nil
}
