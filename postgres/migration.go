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

func addUniqueConstraint(table string, constraint schema.UniqueConstraint) string {
	return "ALTER TABLE " + quote(table) + " ADD CONSTRAINT " + quote(constraint.Name) + " UNIQUE (" + quoteList(constraint.Columns) + ");"
}

func addCheckConstraint(table string, constraint schema.CheckConstraint) string {
	return "ALTER TABLE " + quote(table) + " ADD CONSTRAINT " + quote(constraint.Name) + " CHECK (" + constraint.Expression + ");"
}

func dropConstraint(table, name string) string {
	return "ALTER TABLE " + quote(table) + " DROP CONSTRAINT " + quote(name) + ";"
}
