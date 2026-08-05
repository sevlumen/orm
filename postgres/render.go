package postgres

import (
	"fmt"
	"strings"

	"github.com/sevlumen/orm/schema"
)

// RenderCreateSchema renders deterministic PostgreSQL CREATE TABLE statements.
func RenderCreateSchema(model schema.Schema) (string, error) {
	if err := model.Validate(); err != nil {
		return "", err
	}

	var statements []string
	for _, table := range model.Tables {
		statement, err := renderTable(table)
		if err != nil {
			return "", err
		}
		statements = append(statements, statement)
	}
	return strings.Join(statements, "\n\n") + "\n", nil
}

func renderTable(table schema.Table) (string, error) {
	if len(table.Columns) == 0 {
		return "", fmt.Errorf("postgres: table %q has no columns", table.Name)
	}

	columns := make([]string, 0, len(table.Columns))
	for _, column := range table.Columns {
		columns = append(columns, "    "+renderColumn(column))
	}

	return "CREATE TABLE " + quote(table.Name) + " (\n" +
		strings.Join(columns, ",\n") + "\n);", nil
}

func quote(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}
