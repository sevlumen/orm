package query

import (
	"fmt"
	"sort"
	"strings"
)

type conflictAction uint8

const (
	conflictNone conflictAction = iota
	conflictDoNothing
	conflictDoUpdate
)

// InsertBuilder builds immutable INSERT and UPSERT statements.
type InsertBuilder[T any] struct {
	table              *Table[T]
	rows               [][]Assignment[T]
	conflictConfigured bool
	conflictAny        bool
	conflictTargets    []ConflictColumn[T]
	conflictAction     conflictAction
	conflictUpdates    []Assignment[T]
	returning          bool
	err                error
}

// Insert starts a typed INSERT.
func Insert[T any](table *Table[T]) InsertBuilder[T] {
	builder := InsertBuilder[T]{table: table}
	if table == nil {
		builder.err = fmt.Errorf("query: INSERT requires a table")
	}
	return builder
}

// Row appends one typed row. Each row must assign the same columns.
func (b InsertBuilder[T]) Row(assignments ...Assignment[T]) InsertBuilder[T] {
	if b.err != nil {
		return b
	}
	row := append([]Assignment[T](nil), assignments...)
	b.rows = cloneRows(b.rows)
	b.rows = append(b.rows, row)
	return b
}

// OnConflict selects typed conflict target columns.
func (b InsertBuilder[T]) OnConflict(columns ...ConflictColumn[T]) InsertBuilder[T] {
	if b.conflictConfigured {
		b.err = fmt.Errorf("query: INSERT conflict handling is configured more than once")
		return b
	}
	if len(columns) == 0 {
		b.err = fmt.Errorf("query: ON CONFLICT requires at least one target column")
		return b
	}
	b.conflictConfigured = true
	b.conflictTargets = append([]ConflictColumn[T](nil), columns...)
	return b
}

// OnAnyConflict configures targetless ON CONFLICT, valid only with DO NOTHING.
func (b InsertBuilder[T]) OnAnyConflict() InsertBuilder[T] {
	if b.conflictConfigured {
		b.err = fmt.Errorf("query: INSERT conflict handling is configured more than once")
		return b
	}
	b.conflictConfigured = true
	b.conflictAny = true
	return b
}

// DoNothing completes ON CONFLICT with DO NOTHING.
func (b InsertBuilder[T]) DoNothing() InsertBuilder[T] {
	if !b.conflictConfigured {
		b.err = fmt.Errorf("query: DO NOTHING requires OnConflict or OnAnyConflict")
		return b
	}
	b.conflictAction = conflictDoNothing
	b.conflictUpdates = nil
	return b
}

// DoUpdate completes ON CONFLICT with typed assignments.
func (b InsertBuilder[T]) DoUpdate(assignments ...Assignment[T]) InsertBuilder[T] {
	if !b.conflictConfigured {
		b.err = fmt.Errorf("query: DO UPDATE requires OnConflict")
		return b
	}
	if b.conflictAny {
		b.err = fmt.Errorf("query: targetless ON CONFLICT cannot use DO UPDATE")
		return b
	}
	b.conflictAction = conflictDoUpdate
	b.conflictUpdates = append([]Assignment[T](nil), assignments...)
	return b
}

// Returning appends the table's full typed projection.
func (b InsertBuilder[T]) Returning() InsertBuilder[T] {
	b.returning = true
	return b
}

// Build renders INSERT or UPSERT SQL and positional arguments.
func (b InsertBuilder[T]) Build() (Statement, error) {
	if b.err != nil {
		return Statement{}, b.err
	}
	if b.table == nil {
		return Statement{}, fmt.Errorf("query: INSERT requires a table")
	}
	if len(b.rows) == 0 {
		return Statement{}, fmt.Errorf("query: INSERT requires at least one row")
	}
	if b.conflictConfigured && b.conflictAction == conflictNone {
		return Statement{}, fmt.Errorf("query: ON CONFLICT requires DO NOTHING or DO UPDATE")
	}

	normalizedRows := make([][]Assignment[T], len(b.rows))
	var columns []string
	for rowIndex, row := range b.rows {
		validated, err := validateAssignments(b.table, row, true, false)
		if err != nil {
			return Statement{}, fmt.Errorf("query: INSERT row %d: %w", rowIndex, err)
		}
		sort.Slice(validated, func(i, j int) bool {
			return validated[i].column < validated[j].column
		})
		rowColumns := assignmentColumns(validated)
		if rowIndex == 0 {
			columns = rowColumns
		} else if !equalStrings(columns, rowColumns) {
			return Statement{}, fmt.Errorf("query: INSERT row %d assigns different columns", rowIndex)
		}
		normalizedRows[rowIndex] = validated
	}

	context := &renderContext{}
	rowSQL := make([]string, len(normalizedRows))
	for rowIndex, row := range normalizedRows {
		values := make([]string, len(row))
		for assignmentIndex, assignment := range row {
			value, err := assignment.renderValue(context, false)
			if err != nil {
				return Statement{}, fmt.Errorf("query: INSERT row %d: %w", rowIndex, err)
			}
			values[assignmentIndex] = value
		}
		rowSQL[rowIndex] = "(" + strings.Join(values, ", ") + ")"
	}

	var sql strings.Builder
	sql.WriteString("INSERT INTO ")
	sql.WriteString(quoteIdentifier(b.table.name))
	sql.WriteString(" (")
	sql.WriteString(quoteColumns(columns))
	sql.WriteString(") VALUES ")
	sql.WriteString(strings.Join(rowSQL, ", "))

	if b.conflictConfigured {
		sql.WriteString(" ON CONFLICT")
		if !b.conflictAny {
			targets, err := validateConflictTargets(b.table, b.conflictTargets)
			if err != nil {
				return Statement{}, err
			}
			sql.WriteString(" (")
			sql.WriteString(quoteColumns(targets))
			sql.WriteByte(')')
		}
		switch b.conflictAction {
		case conflictDoNothing:
			sql.WriteString(" DO NOTHING")
		case conflictDoUpdate:
			updates, err := validateAssignments(b.table, b.conflictUpdates, false, true)
			if err != nil {
				return Statement{}, fmt.Errorf("query: ON CONFLICT DO UPDATE: %w", err)
			}
			sort.Slice(updates, func(i, j int) bool {
				return updates[i].column < updates[j].column
			})
			parts := make([]string, len(updates))
			for index, assignment := range updates {
				value, err := assignment.renderValue(context, true)
				if err != nil {
					return Statement{}, err
				}
				parts[index] = quoteIdentifier(assignment.column) + " = " + value
			}
			sql.WriteString(" DO UPDATE SET ")
			sql.WriteString(strings.Join(parts, ", "))
		}
	}
	if b.returning {
		sql.WriteString(" RETURNING ")
		sql.WriteString(quoteColumns(b.table.selectColumns))
	}
	return context.statement(sql.String()), nil
}

func validateConflictTargets[T any](table *Table[T], targets []ConflictColumn[T]) ([]string, error) {
	if len(targets) == 0 {
		return nil, fmt.Errorf("query: ON CONFLICT requires target columns")
	}
	columns := make([]string, len(targets))
	seen := map[string]struct{}{}
	for index, target := range targets {
		if !tableMatches(table, target.table) {
			return nil, fmt.Errorf("query: conflict target %q belongs to a different table", target.column)
		}
		if _, exists := seen[target.column]; exists {
			return nil, fmt.Errorf("query: conflict target repeats column %q", target.column)
		}
		seen[target.column] = struct{}{}
		columns[index] = target.column
	}
	return columns, nil
}

func assignmentColumns[T any](assignments []Assignment[T]) []string {
	columns := make([]string, len(assignments))
	for index, assignment := range assignments {
		columns[index] = assignment.column
	}
	return columns
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func cloneRows[T any](rows [][]Assignment[T]) [][]Assignment[T] {
	result := make([][]Assignment[T], len(rows))
	for index, row := range rows {
		result[index] = append([]Assignment[T](nil), row...)
	}
	return result
}
