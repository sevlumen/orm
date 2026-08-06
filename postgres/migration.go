package postgres

import (
	"fmt"
	"strings"

	"github.com/sevlumen/orm/migration"
	"github.com/sevlumen/orm/schema"
)

// MigrationSQL contains reversible SQL and the planner's risk assessment.
type MigrationSQL struct {
	Up       string
	Down     string
	Risk     migration.Risk
	Warnings []string
}

// RenderMigration renders PostgreSQL up/down SQL for a migration plan.
func RenderMigration(plan migration.Plan) (MigrationSQL, error) {
	if err := plan.Validate(); err != nil {
		return MigrationSQL{}, fmt.Errorf("postgres: invalid migration plan: %w", err)
	}
	up, err := renderOperations(plan.Operations, false)
	if err != nil {
		return MigrationSQL{}, err
	}
	down, err := renderOperations(plan.Operations, true)
	if err != nil {
		return MigrationSQL{}, err
	}
	return MigrationSQL{Up: up, Down: down, Risk: plan.MaxRisk(), Warnings: plan.Warnings()}, nil
}

func renderOperations(operations []migration.Operation, reverse bool) (string, error) {
	if len(operations) == 0 {
		return "-- no schema changes\n", nil
	}

	ordered := orderOperations(operations)
	statements := make([]string, 0, len(ordered))
	if reverse {
		for i := len(ordered) - 1; i >= 0; i-- {
			statement, err := renderOperation(ordered[i], true)
			if err != nil {
				return "", err
			}
			statements = append(statements, statement)
		}
	} else {
		for _, operation := range ordered {
			statement, err := renderOperation(operation, false)
			if err != nil {
				return "", err
			}
			statements = append(statements, statement)
		}
	}
	return strings.Join(statements, "\n") + "\n", nil
}

// orderOperations applies PostgreSQL dependency ordering even for plans built
// manually instead of through migration.Diff. Stable buckets preserve the
// planner's deterministic order within each operation class.
func orderOperations(operations []migration.Operation) []migration.Operation {
	buckets := make([][]migration.Operation, 12)
	for _, operation := range operations {
		priority := 11
		switch operation.Kind {
		case migration.CreateExtension:
			priority = 0
		case migration.CreateEnum:
			priority = 1
		case migration.DropForeignKey:
			priority = 2
		case migration.DropIndex, migration.DropUniqueConstraint, migration.DropCheckConstraint:
			priority = 3
		case migration.DropTable:
			priority = 4
		case migration.CreateTable:
			priority = 5
		case migration.AddColumn:
			priority = 6
		case migration.AlterColumn:
			priority = 7
		case migration.DropColumn:
			priority = 8
		case migration.CreateIndex, migration.AddUniqueConstraint, migration.AddCheckConstraint:
			priority = 9
		case migration.AddForeignKey:
			priority = 10
		case migration.DropEnum:
			priority = 11
		}
		buckets[priority] = append(buckets[priority], operation)
	}

	ordered := make([]migration.Operation, 0, len(operations))
	for _, bucket := range buckets {
		ordered = append(ordered, bucket...)
	}
	return ordered
}

func renderOperation(operation migration.Operation, reverse bool) (string, error) {
	switch operation.Kind {
	case migration.CreateExtension:
		if reverse {
			return "-- extension " + quote(operation.AfterExtension.Name) + " retained during rollback", nil
		}
		return "CREATE EXTENSION IF NOT EXISTS " + quote(operation.AfterExtension.Name) + ";", nil
	case migration.CreateEnum:
		if reverse {
			return "DROP TYPE " + quote(operation.AfterEnum.Name) + ";", nil
		}
		return renderEnum(*operation.AfterEnum), nil
	case migration.DropEnum:
		if reverse {
			return renderEnum(*operation.BeforeEnum), nil
		}
		return "DROP TYPE " + quote(operation.BeforeEnum.Name) + ";", nil
	case migration.CreateTable:
		if reverse {
			return "DROP TABLE " + quote(operation.Table) + ";", nil
		}
		return renderTable(*operation.AfterTable)
	case migration.DropTable:
		if reverse {
			return renderTable(*operation.BeforeTable)
		}
		return "DROP TABLE " + quote(operation.Table) + ";", nil
	case migration.AddColumn:
		if reverse {
			return "ALTER TABLE " + quote(operation.Table) + " DROP COLUMN " + quote(operation.AfterColumn.Name) + ";", nil
		}
		return "ALTER TABLE " + quote(operation.Table) + " ADD COLUMN " + renderColumn(*operation.AfterColumn, operation.AfterColumn.PrimaryKey) + ";", nil
	case migration.DropColumn:
		if reverse {
			return "ALTER TABLE " + quote(operation.Table) + " ADD COLUMN " + renderColumn(*operation.BeforeColumn, operation.BeforeColumn.PrimaryKey) + ";", nil
		}
		return "ALTER TABLE " + quote(operation.Table) + " DROP COLUMN " + quote(operation.BeforeColumn.Name) + ";", nil
	case migration.AlterColumn:
		before, after := operation.BeforeColumn, operation.AfterColumn
		if reverse {
			before, after = after, before
		}
		return renderAlterColumn(operation.Table, *before, *after)
	case migration.CreateIndex:
		if reverse {
			return "DROP INDEX " + quote(operation.AfterIndex.Name) + ";", nil
		}
		return renderIndex(operation.Table, *operation.AfterIndex), nil
	case migration.DropIndex:
		if reverse {
			return renderIndex(operation.Table, *operation.BeforeIndex), nil
		}
		return "DROP INDEX " + quote(operation.BeforeIndex.Name) + ";", nil
	case migration.AddUniqueConstraint:
		if reverse {
			return dropConstraint(operation.Table, operation.AfterUnique.Name), nil
		}
		return addUniqueConstraint(operation.Table, *operation.AfterUnique), nil
	case migration.DropUniqueConstraint:
		if reverse {
			return addUniqueConstraint(operation.Table, *operation.BeforeUnique), nil
		}
		return dropConstraint(operation.Table, operation.BeforeUnique.Name), nil
	case migration.AddCheckConstraint:
		if reverse {
			return dropConstraint(operation.Table, operation.AfterCheck.Name), nil
		}
		return addCheckConstraint(operation.Table, *operation.AfterCheck), nil
	case migration.DropCheckConstraint:
		if reverse {
			return addCheckConstraint(operation.Table, *operation.BeforeCheck), nil
		}
		return dropConstraint(operation.Table, operation.BeforeCheck.Name), nil
	case migration.AddForeignKey:
		if reverse {
			return dropConstraint(operation.Table, operation.AfterForeignKey.Name), nil
		}
		return addForeignKeyConstraint(operation.Table, *operation.AfterForeignKey), nil
	case migration.DropForeignKey:
		if reverse {
			return addForeignKeyConstraint(operation.Table, *operation.BeforeForeignKey), nil
		}
		return dropConstraint(operation.Table, operation.BeforeForeignKey.Name), nil
	default:
		return "", fmt.Errorf("postgres: unsupported migration operation %q", operation.Kind)
	}
}

func renderAlterColumn(table string, before, after schema.Column) (string, error) {
	if before.Name != after.Name {
		return "", fmt.Errorf("postgres: column rename requires an explicit migration")
	}
	if before.Generated != after.Generated {
		return "", fmt.Errorf("postgres: generated-column expression changes require an explicit migration")
	}
	var statements []string
	prefix := "ALTER TABLE " + quote(table) + " ALTER COLUMN " + quote(after.Name)
	if before.Type != after.Type {
		statements = append(statements, prefix+" TYPE "+after.Type+";")
	}
	if before.Nullable != after.Nullable {
		if after.Nullable {
			statements = append(statements, prefix+" DROP NOT NULL;")
		} else {
			statements = append(statements, prefix+" SET NOT NULL;")
		}
	}
	if before.Default != after.Default {
		if after.Default == "" {
			statements = append(statements, prefix+" DROP DEFAULT;")
		} else {
			statements = append(statements, prefix+" SET DEFAULT "+after.Default+";")
		}
	}
	if len(statements) == 0 {
		return "", fmt.Errorf("postgres: alter column %s.%s has no renderable changes", table, after.Name)
	}
	return strings.Join(statements, "\n"), nil
}

func addUniqueConstraint(table string, constraint schema.UniqueConstraint) string {
	return "ALTER TABLE " + quote(table) + " ADD CONSTRAINT " + quote(constraint.Name) + " UNIQUE (" + quoteList(constraint.Columns) + ");"
}

func addCheckConstraint(table string, constraint schema.CheckConstraint) string {
	return "ALTER TABLE " + quote(table) + " ADD CONSTRAINT " + quote(constraint.Name) + " CHECK (" + constraint.Expression + ");"
}

func dropConstraint(table, name string) string {
	return "ALTER TABLE " + quote(table) + " DROP CONSTRAINT " + quote(name) + ";"
}
