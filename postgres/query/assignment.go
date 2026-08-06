package query

import "fmt"

type assignmentKind uint8

const (
	assignmentValue assignmentKind = iota
	assignmentTrustedSQL
	assignmentExcluded
)

// Assignment is one typed INSERT or UPDATE column assignment.
type Assignment[T any] struct {
	table      *Table[T]
	column     string
	kind       assignmentKind
	value      any
	insertable bool
	updatable  bool
	err        error
}

// Set assigns a parameterized value.
func (c Column[T, V]) Set(value V) Assignment[T] {
	if c.table == nil {
		return assignmentError[T](fmt.Errorf("query: column %q has no table", c.name))
	}
	return Assignment[T]{
		table:      c.table,
		column:     c.name,
		kind:       assignmentValue,
		value:      value,
		insertable: c.insertable,
		updatable:  c.updatable,
	}
}

// SetSQL assigns an explicit trusted SQL expression.
func (c Column[T, V]) SetSQL(expression TrustedSQL) Assignment[T] {
	if c.table == nil {
		return assignmentError[T](fmt.Errorf("query: column %q has no table", c.name))
	}
	if err := validateTrustedSQL(expression); err != nil {
		return assignmentError[T](err)
	}
	return Assignment[T]{
		table:      c.table,
		column:     c.name,
		kind:       assignmentTrustedSQL,
		value:      expression,
		insertable: c.insertable,
		updatable:  c.updatable,
	}
}

// Excluded assigns the value from PostgreSQL's EXCLUDED pseudo-table.
// It is valid only in an ON CONFLICT DO UPDATE clause.
func (c Column[T, V]) Excluded() Assignment[T] {
	if c.table == nil {
		return assignmentError[T](fmt.Errorf("query: column %q has no table", c.name))
	}
	return Assignment[T]{
		table:      c.table,
		column:     c.name,
		kind:       assignmentExcluded,
		insertable: c.insertable,
		updatable:  c.updatable,
	}
}

// ConflictColumn is a typed ON CONFLICT target column.
type ConflictColumn[T any] struct {
	table  *Table[T]
	column string
}

// ConflictTarget converts a typed column to an ON CONFLICT target.
func (c Column[T, V]) ConflictTarget() ConflictColumn[T] {
	return ConflictColumn[T]{table: c.table, column: c.name}
}

func assignmentError[T any](err error) Assignment[T] {
	return Assignment[T]{err: err}
}

func (a Assignment[T]) renderValue(context *renderContext, allowExcluded bool) (string, error) {
	if a.err != nil {
		return "", a.err
	}
	switch a.kind {
	case assignmentValue:
		return context.bind(a.value), nil
	case assignmentTrustedSQL:
		expression, ok := a.value.(TrustedSQL)
		if !ok {
			return "", fmt.Errorf("query: assignment for %q has invalid trusted SQL value", a.column)
		}
		return string(expression), nil
	case assignmentExcluded:
		if !allowExcluded {
			return "", fmt.Errorf("query: EXCLUDED assignment for %q is only valid in ON CONFLICT DO UPDATE", a.column)
		}
		return "EXCLUDED." + quoteIdentifier(a.column), nil
	default:
		return "", fmt.Errorf("query: assignment for %q has unsupported kind", a.column)
	}
}

func validateAssignments[T any](table *Table[T], assignments []Assignment[T], insert bool, allowExcluded bool) ([]Assignment[T], error) {
	if len(assignments) == 0 {
		return nil, fmt.Errorf("query: mutation requires at least one assignment")
	}
	result := append([]Assignment[T](nil), assignments...)
	seen := map[string]struct{}{}
	for _, assignment := range result {
		if assignment.err != nil {
			return nil, assignment.err
		}
		if !tableMatches(table, assignment.table) {
			return nil, fmt.Errorf("query: assignment for column %q belongs to a different table", assignment.column)
		}
		if _, exists := seen[assignment.column]; exists {
			return nil, fmt.Errorf("query: column %q is assigned more than once", assignment.column)
		}
		seen[assignment.column] = struct{}{}
		if insert && !assignment.insertable {
			return nil, fmt.Errorf("query: column %q is not insertable", assignment.column)
		}
		if !insert && !assignment.updatable {
			return nil, fmt.Errorf("query: column %q is not updatable", assignment.column)
		}
		if assignment.kind == assignmentExcluded && !allowExcluded {
			return nil, fmt.Errorf("query: EXCLUDED assignment for %q is not valid here", assignment.column)
		}
	}
	return result, nil
}
