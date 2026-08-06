// Package schema defines the database-independent schema representation.
package schema

import (
	"fmt"
	"strings"
)

const maxIdentifierBytes = 63

// Schema is a database-independent representation of application entities.
type Schema struct {
	Extensions []Extension `json:"extensions,omitempty"`
	Enums      []EnumType  `json:"enums,omitempty"`
	Tables     []Table     `json:"tables"`
}

// Table describes a relational table.
type Table struct {
	Name              string             `json:"name"`
	Columns           []Column           `json:"columns"`
	PrimaryKey        *PrimaryKey        `json:"primaryKey,omitempty"`
	UniqueConstraints []UniqueConstraint `json:"uniqueConstraints,omitempty"`
	Checks            []CheckConstraint  `json:"checks,omitempty"`
	ForeignKeys       []ForeignKey       `json:"foreignKeys,omitempty"`
	Indexes           []Index            `json:"indexes,omitempty"`
}

// Column describes a table column. Inline PrimaryKey and Unique flags remain
// available for compact single-column declarations and backward-compatible snapshots.
type Column struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Nullable   bool   `json:"nullable"`
	PrimaryKey bool   `json:"primaryKey,omitempty"`
	Unique     bool   `json:"unique,omitempty"`
	Default    string `json:"default,omitempty"`
	Generated  string `json:"generated,omitempty"`
}

// PrimaryKey describes a named table-level primary key, including composite keys.
type PrimaryKey struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
}

// UniqueConstraint describes a named single or composite unique constraint.
type UniqueConstraint struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
}

// CheckConstraint describes a trusted developer-authored SQL predicate.
type CheckConstraint struct {
	Name       string `json:"name"`
	Expression string `json:"expression"`
}

// Index describes a PostgreSQL-compatible index. Exactly one of Columns or
// Expression must be supplied. Predicate and Expression are trusted schema input.
type Index struct {
	Name       string   `json:"name"`
	Columns    []string `json:"columns,omitempty"`
	Expression string   `json:"expression,omitempty"`
	Include    []string `json:"include,omitempty"`
	Unique     bool     `json:"unique,omitempty"`
	Method     string   `json:"method,omitempty"`
	Predicate  string   `json:"predicate,omitempty"`
}

// Validate checks invariants before SQL rendering or migration planning.
func (s Schema) Validate() error {
	tables := make(map[string]Table, len(s.Tables))
	relationNames := map[string]string{}
	if err := validateNativeSchema(s, relationNames); err != nil {
		return err
	}

	for _, table := range s.Tables {
		if err := validateIdentifier("table", table.Name); err != nil {
			return err
		}
		if _, exists := tables[table.Name]; exists {
			return fmt.Errorf("schema: duplicate table %q", table.Name)
		}
		tables[table.Name] = table
		if previous, exists := relationNames[table.Name]; exists {
			return fmt.Errorf("schema: table %q conflicts with %s", table.Name, previous)
		}
		relationNames[table.Name] = "another relation"
		if len(table.Columns) == 0 {
			return fmt.Errorf("schema: table %q has no columns", table.Name)
		}

		seenColumns := map[string]Column{}
		inlinePrimaryKeys := 0
		for _, column := range table.Columns {
			if err := validateIdentifier("column", column.Name); err != nil {
				return fmt.Errorf("schema: table %q: %w", table.Name, err)
			}
			if strings.TrimSpace(column.Type) == "" {
				return fmt.Errorf("schema: column %q.%q has no type", table.Name, column.Name)
			}
			if strings.ContainsRune(column.Type, '\x00') {
				return fmt.Errorf("schema: column %q.%q type contains NUL", table.Name, column.Name)
			}
			if strings.ContainsRune(column.Generated, '\x00') {
				return fmt.Errorf("schema: generated expression for column %q.%q contains NUL", table.Name, column.Name)
			}
			if strings.TrimSpace(column.Generated) != "" && strings.TrimSpace(column.Default) != "" {
				return fmt.Errorf("schema: generated column %q.%q cannot also have a default", table.Name, column.Name)
			}
			if _, exists := seenColumns[column.Name]; exists {
				return fmt.Errorf("schema: duplicate column %q.%q", table.Name, column.Name)
			}
			seenColumns[column.Name] = column
			if column.PrimaryKey {
				inlinePrimaryKeys++
				if column.Nullable {
					return fmt.Errorf("schema: primary key column %q.%q cannot be nullable", table.Name, column.Name)
				}
			}
		}
		if table.PrimaryKey != nil && inlinePrimaryKeys > 0 {
			return fmt.Errorf("schema: table %q mixes inline and table-level primary keys", table.Name)
		}

		constraintNames := map[string]string{}
		if table.PrimaryKey != nil {
			if err := validateConstraintColumns(table.Name, "primary key", table.PrimaryKey.Name, table.PrimaryKey.Columns, seenColumns, true); err != nil {
				return err
			}
			constraintNames[table.PrimaryKey.Name] = "primary key"
			if previous, exists := relationNames[table.PrimaryKey.Name]; exists {
				return fmt.Errorf("schema: primary key %q on table %q conflicts with %s", table.PrimaryKey.Name, table.Name, previous)
			}
			relationNames[table.PrimaryKey.Name] = "another relation"
		}
		for _, unique := range table.UniqueConstraints {
			if err := validateConstraintColumns(table.Name, "unique constraint", unique.Name, unique.Columns, seenColumns, false); err != nil {
				return err
			}
			if previous, exists := constraintNames[unique.Name]; exists {
				return fmt.Errorf("schema: constraint %q on table %q duplicates %s", unique.Name, table.Name, previous)
			}
			constraintNames[unique.Name] = "unique constraint"
			if previous, exists := relationNames[unique.Name]; exists {
				return fmt.Errorf("schema: unique constraint %q on table %q conflicts with %s", unique.Name, table.Name, previous)
			}
			relationNames[unique.Name] = "another relation"
		}
		for _, check := range table.Checks {
			if err := validateIdentifier("check constraint", check.Name); err != nil {
				return fmt.Errorf("schema: table %q: %w", table.Name, err)
			}
			if strings.TrimSpace(check.Expression) == "" {
				return fmt.Errorf("schema: check constraint %q on table %q has no expression", check.Name, table.Name)
			}
			if strings.ContainsRune(check.Expression, '\x00') {
				return fmt.Errorf("schema: check constraint %q on table %q contains NUL", check.Name, table.Name)
			}
			if previous, exists := constraintNames[check.Name]; exists {
				return fmt.Errorf("schema: constraint %q on table %q duplicates %s", check.Name, table.Name, previous)
			}
			constraintNames[check.Name] = "check constraint"
		}
		for _, foreignKey := range table.ForeignKeys {
			if err := validateForeignKeyShape(table.Name, foreignKey, seenColumns); err != nil {
				return err
			}
			if previous, exists := constraintNames[foreignKey.Name]; exists {
				return fmt.Errorf("schema: constraint %q on table %q duplicates %s", foreignKey.Name, table.Name, previous)
			}
			constraintNames[foreignKey.Name] = "foreign key"
		}
		for _, index := range table.Indexes {
			if err := validateIndex(table.Name, index, seenColumns); err != nil {
				return err
			}
			if previous, exists := relationNames[index.Name]; exists {
				return fmt.Errorf("schema: index %q on table %q conflicts with %s", index.Name, table.Name, previous)
			}
			relationNames[index.Name] = "another relation"
		}
	}

	return validateForeignKeyReferences(s.Tables, tables)
}

func validateIdentifier(kind, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("schema: %s name is required", kind)
	}
	if len(value) > maxIdentifierBytes {
		return fmt.Errorf("schema: %s name %q exceeds %d bytes", kind, value, maxIdentifierBytes)
	}
	if strings.ContainsRune(value, '\x00') {
		return fmt.Errorf("schema: %s name %q contains NUL", kind, value)
	}
	return nil
}

func validateConstraintColumns(table, kind, name string, columns []string, available map[string]Column, requireNotNull bool) error {
	if err := validateIdentifier(kind, name); err != nil {
		return fmt.Errorf("schema: table %q: %w", table, err)
	}
	if len(columns) == 0 {
		return fmt.Errorf("schema: %s %q on table %q has no columns", kind, name, table)
	}
	seen := map[string]struct{}{}
	for _, columnName := range columns {
		column, exists := available[columnName]
		if !exists {
			return fmt.Errorf("schema: %s %q on table %q references unknown column %q", kind, name, table, columnName)
		}
		if _, duplicate := seen[columnName]; duplicate {
			return fmt.Errorf("schema: %s %q on table %q repeats column %q", kind, name, table, columnName)
		}
		seen[columnName] = struct{}{}
		if requireNotNull && column.Nullable {
			return fmt.Errorf("schema: primary key %q on table %q uses nullable column %q", name, table, columnName)
		}
	}
	return nil
}

func validateIndex(table string, index Index, available map[string]Column) error {
	if err := validateIdentifier("index", index.Name); err != nil {
		return fmt.Errorf("schema: table %q: %w", table, err)
	}
	hasColumns := len(index.Columns) > 0
	hasExpression := strings.TrimSpace(index.Expression) != ""
	if hasColumns == hasExpression {
		return fmt.Errorf("schema: index %q on table %q must define exactly one of columns or expression", index.Name, table)
	}
	if strings.ContainsRune(index.Expression, '\x00') || strings.ContainsRune(index.Predicate, '\x00') {
		return fmt.Errorf("schema: index %q on table %q contains NUL", index.Name, table)
	}
	if index.Method != "" {
		for position, char := range index.Method {
			validStart := char == '_' || char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z'
			validRest := validStart || char >= '0' && char <= '9'
			if position == 0 && validStart || position > 0 && validRest {
				continue
			}
			return fmt.Errorf("schema: index %q on table %q has invalid method %q", index.Name, table, index.Method)
		}
	}
	seen := map[string]struct{}{}
	for _, column := range index.Columns {
		if _, exists := available[column]; !exists {
			return fmt.Errorf("schema: index %q on table %q references unknown column %q", index.Name, table, column)
		}
		if _, duplicate := seen[column]; duplicate {
			return fmt.Errorf("schema: index %q on table %q repeats column %q", index.Name, table, column)
		}
		seen[column] = struct{}{}
	}
	for _, column := range index.Include {
		if _, exists := available[column]; !exists {
			return fmt.Errorf("schema: index %q on table %q includes unknown column %q", index.Name, table, column)
		}
		if _, duplicate := seen[column]; duplicate {
			return fmt.Errorf("schema: index %q on table %q repeats key/include column %q", index.Name, table, column)
		}
		seen[column] = struct{}{}
	}
	return nil
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
