package schema

import "fmt"

// Schema is a database-agnostic representation of application entities.
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
	seenTables := map[string]struct{}{}
	for _, table := range s.Tables {
		if table.Name == "" {
			return fmt.Errorf("schema: table name is required")
		}
		if _, exists := seenTables[table.Name]; exists {
			return fmt.Errorf("schema: duplicate table %q", table.Name)
		}
		seenTables[table.Name] = struct{}{}

		seenColumns := map[string]struct{}{}
		for _, column := range table.Columns {
			if column.Name == "" {
				return fmt.Errorf("schema: table %q has a column without a name", table.Name)
			}
			if column.Type == "" {
				return fmt.Errorf("schema: column %q.%q has no type", table.Name, column.Name)
			}
			if _, exists := seenColumns[column.Name]; exists {
				return fmt.Errorf("schema: duplicate column %q.%q", table.Name, column.Name)
			}
			seenColumns[column.Name] = struct{}{}
		}
	}
	return nil
}
