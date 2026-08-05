package entity

import (
	"fmt"
	"strings"

	"github.com/sevlumen/orm/schema"
)

// Configurer adds table-level constraints and indexes after entity fields are parsed.
type Configurer interface {
	ConfigureORM(*TableBuilder)
}

// TableBuilder configures one entity table. Field arguments accept either the
// exported Go field name or the resolved database column name.
type TableBuilder struct {
	table   *schema.Table
	fields  map[string]string
	problem error
}

// IndexBuilder configures one index created by TableBuilder.
type IndexBuilder struct {
	owner *TableBuilder
	index int
}

// PrimaryKey defines a named single or composite primary key.
func (b *TableBuilder) PrimaryKey(name string, fields ...string) {
	if b.problem != nil {
		return
	}
	if b.table.PrimaryKey != nil {
		b.problem = fmt.Errorf("entity: table %q primary key is configured more than once", b.table.Name)
		return
	}
	columns := b.resolve(fields...)
	if b.problem != nil {
		return
	}
	b.table.PrimaryKey = &schema.PrimaryKey{Name: name, Columns: columns}
}

// Unique defines a named single or composite unique constraint.
func (b *TableBuilder) Unique(name string, fields ...string) {
	columns := b.resolve(fields...)
	if b.problem != nil {
		return
	}
	b.table.UniqueConstraints = append(b.table.UniqueConstraints, schema.UniqueConstraint{Name: name, Columns: columns})
}

// Check defines a named trusted SQL check expression.
func (b *TableBuilder) Check(name, expression string) {
	if b.problem != nil {
		return
	}
	b.table.Checks = append(b.table.Checks, schema.CheckConstraint{Name: name, Expression: expression})
}

// Index defines a column or composite index.
func (b *TableBuilder) Index(name string, fields ...string) *IndexBuilder {
	columns := b.resolve(fields...)
	if b.problem != nil {
		return &IndexBuilder{owner: b, index: -1}
	}
	b.table.Indexes = append(b.table.Indexes, schema.Index{Name: name, Columns: columns})
	return &IndexBuilder{owner: b, index: len(b.table.Indexes) - 1}
}

// ExpressionIndex defines an index over a trusted SQL expression.
func (b *TableBuilder) ExpressionIndex(name, expression string) *IndexBuilder {
	if b.problem != nil {
		return &IndexBuilder{owner: b, index: -1}
	}
	b.table.Indexes = append(b.table.Indexes, schema.Index{Name: name, Expression: expression})
	return &IndexBuilder{owner: b, index: len(b.table.Indexes) - 1}
}

// Unique marks the index unique.
func (b *IndexBuilder) Unique() *IndexBuilder {
	if index := b.value(); index != nil {
		index.Unique = true
	}
	return b
}

// Using selects the PostgreSQL index access method, for example btree or gin.
func (b *IndexBuilder) Using(method string) *IndexBuilder {
	if index := b.value(); index != nil {
		index.Method = method
	}
	return b
}

// Where adds a trusted SQL predicate for a partial index.
func (b *IndexBuilder) Where(predicate string) *IndexBuilder {
	if index := b.value(); index != nil {
		index.Predicate = predicate
	}
	return b
}

// Include adds non-key covering columns.
func (b *IndexBuilder) Include(fields ...string) *IndexBuilder {
	if b.owner == nil || b.owner.problem != nil {
		return b
	}
	columns := b.owner.resolve(fields...)
	if index := b.value(); index != nil {
		index.Include = append(index.Include, columns...)
	}
	return b
}

func (b *IndexBuilder) value() *schema.Index {
	if b == nil || b.owner == nil || b.owner.problem != nil || b.index < 0 || b.index >= len(b.owner.table.Indexes) {
		return nil
	}
	return &b.owner.table.Indexes[b.index]
}

func (b *TableBuilder) resolve(fields ...string) []string {
	if b.problem != nil {
		return nil
	}
	if len(fields) == 0 {
		b.problem = fmt.Errorf("entity: table configuration requires at least one field")
		return nil
	}
	columns := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		column, exists := b.fields[field]
		if !exists {
			b.problem = fmt.Errorf("entity: table %q configuration references unknown field or column %q", b.table.Name, field)
			return nil
		}
		columns = append(columns, column)
	}
	return columns
}
