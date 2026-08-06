package query

import (
	"fmt"
	"strings"
)

type predicateRenderer func(*renderContext) (string, error)

// Predicate is an immutable typed boolean expression for one table.
type Predicate[T any] struct {
	table  *Table[T]
	render predicateRenderer
	err    error
}

// Eq compares a column to a parameterized value.
func (c Column[T, V]) Eq(value V) Predicate[T] {
	return c.compareValue("=", value)
}

// Ne compares a column to a parameterized value.
func (c Column[T, V]) Ne(value V) Predicate[T] {
	return c.compareValue("<>", value)
}

// Lt compares a column to a parameterized value.
func (c Column[T, V]) Lt(value V) Predicate[T] {
	return c.compareValue("<", value)
}

// Lte compares a column to a parameterized value.
func (c Column[T, V]) Lte(value V) Predicate[T] {
	return c.compareValue("<=", value)
}

// Gt compares a column to a parameterized value.
func (c Column[T, V]) Gt(value V) Predicate[T] {
	return c.compareValue(">", value)
}

// Gte compares a column to a parameterized value.
func (c Column[T, V]) Gte(value V) Predicate[T] {
	return c.compareValue(">=", value)
}

func (c Column[T, V]) compareValue(operator string, value V) Predicate[T] {
	if c.table == nil {
		return predicateError[T](fmt.Errorf("query: column %q has no table", c.name))
	}
	return Predicate[T]{
		table: c.table,
		render: func(context *renderContext) (string, error) {
			return quoteIdentifier(c.name) + " " + operator + " " + context.bind(value), nil
		},
	}
}

// EqColumn compares two columns from the same table.
func (c Column[T, V]) EqColumn(other Column[T, V]) Predicate[T] {
	return c.compareColumn("=", other)
}

// NeColumn compares two columns from the same table.
func (c Column[T, V]) NeColumn(other Column[T, V]) Predicate[T] {
	return c.compareColumn("<>", other)
}

func (c Column[T, V]) compareColumn(operator string, other Column[T, V]) Predicate[T] {
	if !tableMatches(c.table, other.table) {
		return predicateError[T](fmt.Errorf("query: columns %q and %q belong to different tables", c.name, other.name))
	}
	return Predicate[T]{
		table: c.table,
		render: func(*renderContext) (string, error) {
			return quoteIdentifier(c.name) + " " + operator + " " + quoteIdentifier(other.name), nil
		},
	}
}

// In compares a column against one or more parameterized values.
func (c Column[T, V]) In(values ...V) Predicate[T] {
	if c.table == nil {
		return predicateError[T](fmt.Errorf("query: column %q has no table", c.name))
	}
	if len(values) == 0 {
		return predicateError[T](fmt.Errorf("query: IN predicate for %q.%q requires at least one value", c.table.name, c.name))
	}
	copyValues := append([]V(nil), values...)
	return Predicate[T]{
		table: c.table,
		render: func(context *renderContext) (string, error) {
			placeholders := make([]string, len(copyValues))
			for index, value := range copyValues {
				placeholders[index] = context.bind(value)
			}
			return quoteIdentifier(c.name) + " IN (" + strings.Join(placeholders, ", ") + ")", nil
		},
	}
}

// IsNull checks a column for SQL NULL.
func (c Column[T, V]) IsNull() Predicate[T] {
	if c.table == nil {
		return predicateError[T](fmt.Errorf("query: column %q has no table", c.name))
	}
	return Predicate[T]{table: c.table, render: func(*renderContext) (string, error) {
		return quoteIdentifier(c.name) + " IS NULL", nil
	}}
}

// IsNotNull checks a column for a non-NULL value.
func (c Column[T, V]) IsNotNull() Predicate[T] {
	if c.table == nil {
		return predicateError[T](fmt.Errorf("query: column %q has no table", c.name))
	}
	return Predicate[T]{table: c.table, render: func(*renderContext) (string, error) {
		return quoteIdentifier(c.name) + " IS NOT NULL", nil
	}}
}

// TrustedPredicate inserts an explicit trusted SQL predicate for one table.
func TrustedPredicate[T any](table *Table[T], expression TrustedSQL) Predicate[T] {
	if table == nil {
		return predicateError[T](fmt.Errorf("query: trusted predicate requires a table"))
	}
	if err := validateTrustedSQL(expression); err != nil {
		return predicateError[T](err)
	}
	text := string(expression)
	return Predicate[T]{table: table, render: func(*renderContext) (string, error) {
		return text, nil
	}}
}

// And combines predicates from the same table.
func And[T any](predicates ...Predicate[T]) Predicate[T] {
	return combinePredicates("AND", predicates...)
}

// Or combines predicates from the same table.
func Or[T any](predicates ...Predicate[T]) Predicate[T] {
	return combinePredicates("OR", predicates...)
}

func combinePredicates[T any](operator string, predicates ...Predicate[T]) Predicate[T] {
	if len(predicates) == 0 {
		return predicateError[T](fmt.Errorf("query: %s requires at least one predicate", operator))
	}
	table := predicates[0].table
	for _, predicate := range predicates {
		if predicate.err != nil {
			return predicateError[T](predicate.err)
		}
		if !tableMatches(table, predicate.table) {
			return predicateError[T](fmt.Errorf("query: %s predicates belong to different tables", operator))
		}
	}
	copyPredicates := append([]Predicate[T](nil), predicates...)
	return Predicate[T]{
		table: table,
		render: func(context *renderContext) (string, error) {
			parts := make([]string, len(copyPredicates))
			for index, predicate := range copyPredicates {
				part, err := predicate.renderExpression(context)
				if err != nil {
					return "", err
				}
				parts[index] = "(" + part + ")"
			}
			return strings.Join(parts, " "+operator+" "), nil
		},
	}
}

// Not negates one predicate.
func Not[T any](predicate Predicate[T]) Predicate[T] {
	if predicate.err != nil {
		return predicateError[T](predicate.err)
	}
	return Predicate[T]{
		table: predicate.table,
		render: func(context *renderContext) (string, error) {
			part, err := predicate.renderExpression(context)
			if err != nil {
				return "", err
			}
			return "NOT (" + part + ")", nil
		},
	}
}

func predicateError[T any](err error) Predicate[T] {
	return Predicate[T]{err: err}
}

func (p Predicate[T]) renderFor(table *Table[T], context *renderContext) (string, error) {
	if p.err != nil {
		return "", p.err
	}
	if !tableMatches(table, p.table) {
		return "", fmt.Errorf("query: predicate belongs to a different table")
	}
	return p.renderExpression(context)
}

func (p Predicate[T]) renderExpression(context *renderContext) (string, error) {
	if p.err != nil {
		return "", p.err
	}
	if p.render == nil {
		return "", fmt.Errorf("query: predicate is empty")
	}
	return p.render(context)
}

// Order is a typed ORDER BY expression.
type Order[T any] struct {
	table     *Table[T]
	column    string
	direction string
	err       error
}

// Asc orders a column ascending.
func (c Column[T, V]) Asc() Order[T] {
	return Order[T]{table: c.table, column: c.name, direction: "ASC"}
}

// Desc orders a column descending.
func (c Column[T, V]) Desc() Order[T] {
	return Order[T]{table: c.table, column: c.name, direction: "DESC"}
}

func (o Order[T]) renderFor(table *Table[T]) (string, error) {
	if o.err != nil {
		return "", o.err
	}
	if !tableMatches(table, o.table) {
		return "", fmt.Errorf("query: order column %q belongs to a different table", o.column)
	}
	return quoteIdentifier(o.column) + " " + o.direction, nil
}
