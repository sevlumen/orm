package orm

import (
	"fmt"

	"github.com/sevlumen/orm/entity"
	"github.com/sevlumen/orm/migration"
	"github.com/sevlumen/orm/postgres"
)

// BuildSnapshot creates a versioned schema snapshot from Go entities.
func BuildSnapshot(entities ...any) (migration.Snapshot, error) {
	model, err := entity.Parse(entities...)
	if err != nil {
		return migration.Snapshot{}, err
	}
	return migration.NewSnapshot(model)
}

// PostgreSQLMigration compares a previous snapshot with current entities.
func PostgreSQLMigration(before migration.Snapshot, entities ...any) (postgres.MigrationSQL, migration.Snapshot, error) {
	after, err := BuildSnapshot(entities...)
	if err != nil {
		return postgres.MigrationSQL{}, migration.Snapshot{}, err
	}
	plan, err := migration.Diff(before, after)
	if err != nil {
		return postgres.MigrationSQL{}, migration.Snapshot{}, fmt.Errorf("orm: plan migration: %w", err)
	}
	sql, err := postgres.RenderMigration(plan)
	if err != nil {
		return postgres.MigrationSQL{}, migration.Snapshot{}, fmt.Errorf("orm: render migration: %w", err)
	}
	return sql, after, nil
}
