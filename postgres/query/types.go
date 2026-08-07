// Package query provides immutable, type-safe PostgreSQL statement builders.
package query

import (
	"fmt"
	"strings"
)

// RowScanner is implemented by sql.Row and sql.Rows.
type RowScanner interface {
	Scan(dest ...any) error
}

// ScanFunc scans one database row into a generated entity value.
type ScanFunc[T any] func(RowScanner) (T, error)

// Table is typed metadata for one PostgreSQL table.
type Table[T any] struct {
	name          string
	selectColumns []string
	scan          ScanFunc[T]
}

// NewTable validates and creates typed table metadata.
func NewTable[T any](name string, selectColumns []string, scan ScanFunc[T]) (*Table[T], error) {
	if err := validateIdentifier("table", name); err != nil {
		return nil, err
	}
	if len(selectColumns) == 0 {
		return nil, fmt.Errorf("query: table %q requires at least one selectable column", name)
	}
	if scan == nil {
		return nil, fmt.Errorf("query: table %q requires a scan function", name)
	}
	columns := make([]string, len(selectColumns))
	seen := map[string]struct{}{}
	for index, column := range selectColumns {
		if err := validateIdentifier("column", column); err != nil {
			return nil, fmt.Errorf("query: table %q: %w", name, err)
		}
		if _, exists := seen[column]; exists {
			return nil, fmt.Errorf("query: table %q repeats selectable column %q", name, column)
		}
		seen[column] = struct{}{}
		columns[index] = column
	}
	return &Table[T]{name: name, selectColumns: columns, scan: scan}, nil
}

// Name returns the unquoted database table name.
func (t *Table[T]) Name() string {
	if t == nil {
		return ""
	}
	return t.name
}

// Scan scans one row using the generated table scanner.
func (t *Table[T]) Scan(row RowScanner) (T, error) {
	var zero T
	if t == nil || t.scan == nil {
		return zero, fmt.Errorf("query: table scanner is not configured")
	}
	if row == nil {
		return zero, fmt.Errorf("query: row scanner is nil")
	}
	return t.scan(row)
}

// ColumnOptions controls whether a column may participate in mutations.
type ColumnOptions struct {
	Insertable bool
	Updatable  bool
}

// MutableColumn returns the default insertable and updatable options.
func MutableColumn() ColumnOptions {
	return ColumnOptions{Insertable: true, Updatable: true}
}

// ReadOnlyColumn marks a selectable column as neither insertable nor updatable.
func ReadOnlyColumn() ColumnOptions {
	return ColumnOptions{}
}

// InsertOnlyColumn marks a column insertable but not updatable.
func InsertOnlyColumn() ColumnOptions {
	return ColumnOptions{Insertable: true}
}

// UpdateOnlyColumn marks a column updatable but not insertable.
func UpdateOnlyColumn() ColumnOptions {
	return ColumnOptions{Updatable: true}
}

// Column is typed metadata for one table column.
type Column[T, V any] struct {
	table      *Table[T]
	name       string
	insertable bool
	updatable  bool
}

// NewColumn validates and creates a typed column. Omit options for a mutable column.
func NewColumn[T, V any](table *Table[T], name string, options ...ColumnOptions) (Column[T, V], error) {
	if table == nil {
		return Column[T, V]{}, fmt.Errorf("query: column %q requires a table", name)
	}
	if err := validateIdentifier("column", name); err != nil {
		return Column[T, V]{}, fmt.Errorf("query: table %q: %w", table.name, err)
	}
	if len(options) > 1 {
		return Column[T, V]{}, fmt.Errorf("query: column %q.%q received multiple option sets", table.name, name)
	}
	config := MutableColumn()
	if len(options) == 1 {
		config = options[0]
	}
	return Column[T, V]{
		table:      table,
		name:       name,
		insertable: config.Insertable,
		updatable:  config.Updatable,
	}, nil
}

// Name returns the unquoted database column name.
func (c Column[T, V]) Name() string { return c.name }

// Table returns the typed owner table.
func (c Column[T, V]) Table() *Table[T] { return c.table }

// TrustedSQL is an explicit developer-authored SQL expression escape hatch.
// Never construct TrustedSQL from request or tenant data.
type TrustedSQL string

// Statement contains rendered SQL and positional arguments.
type Statement struct {
	SQL  string
	Args []any
}

func validateIdentifier(kind, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("query: %s name is required", kind)
	}
	if len(value) > 63 {
		return fmt.Errorf("query: %s name %q exceeds 63 bytes", kind, value)
	}
	if strings.ContainsRune(value, '\x00') {
		return fmt.Errorf("query: %s name %q contains NUL", kind, value)
	}
	return nil
}

func quoteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func quoteColumns(columns []string) string {
	quoted := make([]string, len(columns))
	for index, column := range columns {
		quoted[index] = quoteIdentifier(column)
	}
	return strings.Join(quoted, ", ")
}

func tableMatches[T any](left, right *Table[T]) bool {
	return left != nil && left == right
}
