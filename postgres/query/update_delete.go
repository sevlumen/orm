package query

import (
	"fmt"
	"sort"
	"strings"
)

// UpdateBuilder builds immutable UPDATE statements.
type UpdateBuilder[T any] struct {
	table       *Table[T]
	assignments []Assignment[T]
	predicate   *Predicate[T]
	allRows     bool
	returning   bool
	err         error
}

// Update starts a typed UPDATE.
func Update[T any](table *Table[T]) UpdateBuilder[T] {
	builder := UpdateBuilder[T]{table: table}
	if table == nil {
		builder.err = fmt.Errorf("query: UPDATE requires a table")
	}
	return builder
}

// Set appends typed assignments.
func (b UpdateBuilder[T]) Set(assignments ...Assignment[T]) UpdateBuilder[T] {
	if b.err != nil {
		return b
	}
	b.assignments = append(append([]Assignment[T](nil), b.assignments...), assignments...)
	return b
}

// Where adds a predicate. Repeated calls are combined with AND.
func (b UpdateBuilder[T]) Where(predicate Predicate[T]) UpdateBuilder[T] {
	if b.err != nil {
		return b
	}
	if b.predicate == nil {
		copyPredicate := predicate
		b.predicate = &copyPredicate
		return b
	}
	combined := And(*b.predicate, predicate)
	b.predicate = &combined
	return b
}

// AllRows explicitly opts into updating every row.
func (b UpdateBuilder[T]) AllRows() UpdateBuilder[T] {
	b.allRows = true
	return b
}

// Returning appends the table's full typed projection.
func (b UpdateBuilder[T]) Returning() UpdateBuilder[T] {
	b.returning = true
	return b
}

// Build renders UPDATE SQL and positional arguments.
func (b UpdateBuilder[T]) Build() (Statement, error) {
	if b.err != nil {
		return Statement{}, b.err
	}
	if b.table == nil {
		return Statement{}, fmt.Errorf("query: UPDATE requires a table")
	}
	if b.predicate == nil && !b.allRows {
		return Statement{}, fmt.Errorf("query: UPDATE requires WHERE or explicit AllRows")
	}
	if b.predicate != nil && b.allRows {
		return Statement{}, fmt.Errorf("query: UPDATE cannot combine WHERE with AllRows")
	}

	assignments, err := validateAssignments(b.table, b.assignments, false, false)
	if err != nil {
		return Statement{}, fmt.Errorf("query: UPDATE: %w", err)
	}
	sort.Slice(assignments, func(i, j int) bool {
		return assignments[i].column < assignments[j].column
	})
	context := &renderContext{}
	parts := make([]string, len(assignments))
	for index, assignment := range assignments {
		value, err := assignment.renderValue(context, false)
		if err != nil {
			return Statement{}, err
		}
		parts[index] = quoteIdentifier(assignment.column) + " = " + value
	}

	var sql strings.Builder
	sql.WriteString("UPDATE ")
	sql.WriteString(quoteIdentifier(b.table.name))
	sql.WriteString(" SET ")
	sql.WriteString(strings.Join(parts, ", "))
	if b.predicate != nil {
		predicate, err := b.predicate.renderFor(b.table, context)
		if err != nil {
			return Statement{}, err
		}
		sql.WriteString(" WHERE ")
		sql.WriteString(predicate)
	}
	if b.returning {
		sql.WriteString(" RETURNING ")
		sql.WriteString(quoteColumns(b.table.selectColumns))
	}
	return context.statement(sql.String()), nil
}

// DeleteBuilder builds immutable DELETE statements.
type DeleteBuilder[T any] struct {
	table     *Table[T]
	predicate *Predicate[T]
	allRows   bool
	returning bool
	err       error
}

// Delete starts a typed DELETE.
func Delete[T any](table *Table[T]) DeleteBuilder[T] {
	builder := DeleteBuilder[T]{table: table}
	if table == nil {
		builder.err = fmt.Errorf("query: DELETE requires a table")
	}
	return builder
}

// Where adds a predicate. Repeated calls are combined with AND.
func (b DeleteBuilder[T]) Where(predicate Predicate[T]) DeleteBuilder[T] {
	if b.err != nil {
		return b
	}
	if b.predicate == nil {
		copyPredicate := predicate
		b.predicate = &copyPredicate
		return b
	}
	combined := And(*b.predicate, predicate)
	b.predicate = &combined
	return b
}

// AllRows explicitly opts into deleting every row.
func (b DeleteBuilder[T]) AllRows() DeleteBuilder[T] {
	b.allRows = true
	return b
}

// Returning appends the table's full typed projection.
func (b DeleteBuilder[T]) Returning() DeleteBuilder[T] {
	b.returning = true
	return b
}

// Build renders DELETE SQL and positional arguments.
func (b DeleteBuilder[T]) Build() (Statement, error) {
	if b.err != nil {
		return Statement{}, b.err
	}
	if b.table == nil {
		return Statement{}, fmt.Errorf("query: DELETE requires a table")
	}
	if b.predicate == nil && !b.allRows {
		return Statement{}, fmt.Errorf("query: DELETE requires WHERE or explicit AllRows")
	}
	if b.predicate != nil && b.allRows {
		return Statement{}, fmt.Errorf("query: DELETE cannot combine WHERE with AllRows")
	}

	context := &renderContext{}
	var sql strings.Builder
	sql.WriteString("DELETE FROM ")
	sql.WriteString(quoteIdentifier(b.table.name))
	if b.predicate != nil {
		predicate, err := b.predicate.renderFor(b.table, context)
		if err != nil {
			return Statement{}, err
		}
		sql.WriteString(" WHERE ")
		sql.WriteString(predicate)
	}
	if b.returning {
		sql.WriteString(" RETURNING ")
		sql.WriteString(quoteColumns(b.table.selectColumns))
	}
	return context.statement(sql.String()), nil
}
