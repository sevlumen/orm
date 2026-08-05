// Package schema defines the database-independent schema representation.
package schema

import (
	"fmt"
	"strings"
)

// Schema is a database-independent representation of application entities.
type Schema struct {
	Tables []Table
}

// Table describes a relational table.
type Table struct {
	Name    string
	Columns []Column
}

// Column describes a table column.
type Column struct {
	Name       string
	Type       string
	Nullable   bool
	PrimaryKey bool
	Unique     bool
	Default    string
}

// Validate checks invariants before SQL rendering.
func (s Schema) Validate() error {
	if len(s.Tables) == 0 {
		return fmt.Errorf("schema: at least one table is required")
	}

	seenTables := map[string]struct{}{}
	for _, table := range s.Tables {
		if strings.TrimSpace(table.Name) == "" {
			return fmt.Errorf("schema: table name is required")
		}
		if _, exists := seenTables[table.Name]; exists {
			return fmt.Errorf("schema: duplicate table %q", table.Name)
		}
		seenTables[table.Name] = struct{}{}
		if len(table.Columns) == 0 {
			return fmt.Errorf("schema: table %q has no columns", table.Name)
		}

		seenColumns := map[string]struct{}{}
		primaryKeys := 0
		for _, column := range table.Columns {
			if strings.TrimSpace(column.Name) == "" {
				return fmt.Errorf("schema: table %q has a column without a name", table.Name)
			}
			if strings.TrimSpace(column.Type) == "" {
				return fmt.Errorf("schema: column %q.%q has no type", table.Name, column.Name)
			}
			if _, exists := seenColumns[column.Name]; exists {
				return fmt.Errorf("schema: duplicate column %q.%q", table.Name, column.Name)
			}
			seenColumns[column.Name] = struct{}{}
			if column.PrimaryKey {
				primaryKeys++
				if column.Nullable {
					return fmt.Errorf("schema: primary key column %q.%q cannot be nullable", table.Name, column.Name)
				}
			}
		}
		if primaryKeys > 1 {
			return fmt.Errorf("schema: table %q has multiple inline primary keys; composite keys are not supported yet", table.Name)
		}
	}
	return nil
}
