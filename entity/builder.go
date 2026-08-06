package entity

import (
	"fmt"
	"strings"

	"github.com/sevlumen/orm/schema"
)

// Configurer adds table-level constraints, foreign keys, and indexes after entity fields are parsed.
type Configurer interface {
	ConfigureORM(*TableBuilder)
}

// TableBuilder configures one entity table. Local field arguments accept either
// the exported Go field name or the resolved database column name.
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

// ForeignKeyBuilder configures one foreign key created by TableBuilder.
type ForeignKeyBuilder struct {
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

// ForeignKey starts a named single-column or composite foreign key using local fields.
// Call References on the returned builder to select the referenced table and columns.
func (b *TableBuilder) ForeignKey(name string, fields ...string) *ForeignKeyBuilder {
	columns := b.resolve(fields...)
	if b.problem != nil {
		return &ForeignKeyBuilder{owner: b, index: -1}
	}
	b.table.ForeignKeys = append(b.table.ForeignKeys, schema.ForeignKey{Name: name, Columns: columns})
	return &ForeignKeyBuilder{owner: b, index: len(b.table.ForeignKeys) - 1}
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

// References selects the referenced database table and column names.
func (b *ForeignKeyBuilder) References(table string, columns ...string) *ForeignKeyBuilder {
	foreignKey := b.value()
	if foreignKey == nil {
		return b
	}
	table = strings.TrimSpace(table)
	if table == "" {
		b.owner.problem = fmt.Errorf("entity: foreign key referenced table is required")
		return b
	}
	if len(columns) == 0 {
		b.owner.problem = fmt.Errorf("entity: foreign key references require at least one column")
		return b
	}
	referenced := make([]string, len(columns))
	for i, column := range columns {
		column = strings.TrimSpace(column)
		if column == "" {
			b.owner.problem = fmt.Errorf("entity: foreign key referenced column is required")
			return b
		}
		referenced[i] = column
	}
	foreignKey.ReferencedTable = table
	foreignKey.ReferencedColumns = referenced
	return b
}

// OnDelete configures the PostgreSQL ON DELETE action.
func (b *ForeignKeyBuilder) OnDelete(action schema.ReferentialAction) *ForeignKeyBuilder {
	if foreignKey := b.value(); foreignKey != nil {
		foreignKey.OnDelete = action
	}
	return b
}

// OnUpdate configures the PostgreSQL ON UPDATE action.
func (b *ForeignKeyBuilder) OnUpdate(action schema.ReferentialAction) *ForeignKeyBuilder {
	if foreignKey := b.value(); foreignKey != nil {
		foreignKey.OnUpdate = action
	}
	return b
}

// Deferrable marks the foreign key DEFERRABLE INITIALLY IMMEDIATE.
func (b *ForeignKeyBuilder) Deferrable() *ForeignKeyBuilder {
	if foreignKey := b.value(); foreignKey != nil {
		foreignKey.Deferrable = true
	}
	return b
}

// InitiallyDeferred marks the foreign key DEFERRABLE INITIALLY DEFERRED.
func (b *ForeignKeyBuilder) InitiallyDeferred() *ForeignKeyBuilder {
	if foreignKey := b.value(); foreignKey != nil {
		foreignKey.Deferrable = true
		foreignKey.InitiallyDeferred = true
	}
	return b
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

func (b *ForeignKeyBuilder) value() *schema.ForeignKey {
	if b == nil || b.owner == nil || b.owner.problem != nil || b.index < 0 || b.index >= len(b.owner.table.ForeignKeys) {
		return nil
	}
	return &b.owner.table.ForeignKeys[b.index]
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
