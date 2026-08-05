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
	statements := make([]string, 0, len(operations))
	if reverse {
		for i := len(operations) - 1; i >= 0; i-- {
			statement, err := renderOperation(operations[i], true)
			if err != nil {
				return "", err
			}
			statements = append(statements, statement)
		}
	} else {
		for _, operation := range operations {
			statement, err := renderOperation(operation, false)
			if err != nil {
				return "", err
			}
			statements = append(statements, statement)
		}
	}
	return strings.Join(statements, "\n") + "\n", nil
}

func renderOperation(operation migration.Operation, reverse bool) (string, error) {
	switch operation.Kind {
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
		return "ALTER TABLE " + quote(operation.Table) + " ADD COLUMN " + renderColumn(*operation.AfterColumn) + ";", nil
	case migration.DropColumn:
		if reverse {
			return "ALTER TABLE " + quote(operation.Table) + " ADD COLUMN " + renderColumn(*operation.BeforeColumn) + ";", nil
		}
		return "ALTER TABLE " + quote(operation.Table) + " DROP COLUMN " + quote(operation.BeforeColumn.Name) + ";", nil
	case migration.AlterColumn:
		before, after := operation.BeforeColumn, operation.AfterColumn
		if reverse {
			before, after = after, before
		}
		return renderAlterColumn(operation.Table, *before, *after)
	default:
		return "", fmt.Errorf("postgres: unsupported migration operation %q", operation.Kind)
	}
}

func renderAlterColumn(table string, before, after schema.Column) (string, error) {
	if before.Name != after.Name {
		return "", fmt.Errorf("postgres: column rename requires an explicit migration")
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

func renderColumn(column schema.Column) string {
	parts := []string{quote(column.Name), column.Type}
	if !column.Nullable {
		parts = append(parts, "NOT NULL")
	}
	if column.Default != "" {
		parts = append(parts, "DEFAULT", column.Default)
	}
	if column.Unique {
		parts = append(parts, "UNIQUE")
	}
	if column.PrimaryKey {
		parts = append(parts, "PRIMARY KEY")
	}
	return strings.Join(parts, " ")
}
