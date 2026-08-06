package query

import (
	"fmt"
	"strings"
)

// SelectBuilder builds one typed PostgreSQL SELECT statement.
type SelectBuilder[T any] struct {
	table     *Table[T]
	predicate *Predicate[T]
	orders    []Order[T]
	limit     *int64
	offset    *int64
	distinct  bool
	lock      string
	lockWait  string
	err       error
}

// Select starts a full-row select using the table's ordered selectable columns.
func Select[T any](table *Table[T]) SelectBuilder[T] {
	builder := SelectBuilder[T]{table: table}
	if table == nil {
		builder.err = fmt.Errorf("query: SELECT requires a table")
	}
	return builder
}

// Distinct adds DISTINCT.
func (b SelectBuilder[T]) Distinct() SelectBuilder[T] {
	b.distinct = true
	return b
}

// Where adds a predicate. Repeated calls are combined with AND.
func (b SelectBuilder[T]) Where(predicate Predicate[T]) SelectBuilder[T] {
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

// OrderBy appends typed order expressions.
func (b SelectBuilder[T]) OrderBy(orders ...Order[T]) SelectBuilder[T] {
	if b.err != nil {
		return b
	}
	b.orders = append(append([]Order[T](nil), b.orders...), orders...)
	return b
}

// Limit applies a parameterized non-negative LIMIT.
func (b SelectBuilder[T]) Limit(value int64) SelectBuilder[T] {
	if value < 0 {
		b.err = fmt.Errorf("query: SELECT limit cannot be negative")
		return b
	}
	b.limit = pointer(value)
	return b
}

// Offset applies a parameterized non-negative OFFSET.
func (b SelectBuilder[T]) Offset(value int64) SelectBuilder[T] {
	if value < 0 {
		b.err = fmt.Errorf("query: SELECT offset cannot be negative")
		return b
	}
	b.offset = pointer(value)
	return b
}

// ForUpdate applies FOR UPDATE row locking.
func (b SelectBuilder[T]) ForUpdate() SelectBuilder[T] {
	return b.withLock("FOR UPDATE")
}

// ForShare applies FOR SHARE row locking.
func (b SelectBuilder[T]) ForShare() SelectBuilder[T] {
	return b.withLock("FOR SHARE")
}

func (b SelectBuilder[T]) withLock(lock string) SelectBuilder[T] {
	if b.lock != "" && b.lock != lock {
		b.err = fmt.Errorf("query: SELECT cannot combine %s with %s", b.lock, lock)
		return b
	}
	b.lock = lock
	return b
}

// SkipLocked applies SKIP LOCKED to a row lock.
func (b SelectBuilder[T]) SkipLocked() SelectBuilder[T] {
	return b.withLockWait("SKIP LOCKED")
}

// NoWait applies NOWAIT to a row lock.
func (b SelectBuilder[T]) NoWait() SelectBuilder[T] {
	return b.withLockWait("NOWAIT")
}

func (b SelectBuilder[T]) withLockWait(wait string) SelectBuilder[T] {
	if b.lockWait != "" && b.lockWait != wait {
		b.err = fmt.Errorf("query: SELECT cannot combine %s with %s", b.lockWait, wait)
		return b
	}
	b.lockWait = wait
	return b
}

// Build renders SQL and positional arguments.
func (b SelectBuilder[T]) Build() (Statement, error) {
	if b.err != nil {
		return Statement{}, b.err
	}
	if b.table == nil {
		return Statement{}, fmt.Errorf("query: SELECT requires a table")
	}
	if b.lockWait != "" && b.lock == "" {
		return Statement{}, fmt.Errorf("query: SELECT %s requires FOR UPDATE or FOR SHARE", b.lockWait)
	}

	context := &renderContext{}
	var sql strings.Builder
	sql.WriteString("SELECT ")
	if b.distinct {
		sql.WriteString("DISTINCT ")
	}
	sql.WriteString(quoteColumns(b.table.selectColumns))
	sql.WriteString(" FROM ")
	sql.WriteString(quoteIdentifier(b.table.name))

	if b.predicate != nil {
		predicate, err := b.predicate.renderFor(b.table, context)
		if err != nil {
			return Statement{}, err
		}
		sql.WriteString(" WHERE ")
		sql.WriteString(predicate)
	}
	if len(b.orders) > 0 {
		parts := make([]string, len(b.orders))
		for index, order := range b.orders {
			part, err := order.renderFor(b.table)
			if err != nil {
				return Statement{}, err
			}
			parts[index] = part
		}
		sql.WriteString(" ORDER BY ")
		sql.WriteString(strings.Join(parts, ", "))
	}
	if b.limit != nil {
		sql.WriteString(" LIMIT ")
		sql.WriteString(context.bind(*b.limit))
	}
	if b.offset != nil {
		sql.WriteString(" OFFSET ")
		sql.WriteString(context.bind(*b.offset))
	}
	if b.lock != "" {
		sql.WriteByte(' ')
		sql.WriteString(b.lock)
		if b.lockWait != "" {
			sql.WriteByte(' ')
			sql.WriteString(b.lockWait)
		}
	}
	return context.statement(sql.String()), nil
}

func pointer[T any](value T) *T { return &value }
