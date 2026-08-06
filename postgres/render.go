package postgres

import (
	"fmt"
	"strings"

	"github.com/sevlumen/orm/schema"
)

// RenderCreateSchema renders deterministic PostgreSQL CREATE statements.
func RenderCreateSchema(model schema.Schema) (string, error) {
	if err := model.Validate(); err != nil {
		return "", err
	}
	var statements []string
	for _, extension := range model.Extensions {
		statements = append(statements, "CREATE EXTENSION IF NOT EXISTS "+quote(extension.Name)+";")
	}
	for _, enum := range model.Enums {
		statements = append(statements, renderEnum(enum))
	}
	for _, table := range model.Tables {
		statement, err := renderTable(table)
		if err != nil {
			return "", err
		}
		statements = append(statements, statement)
	}
	// Foreign keys are deliberately added after every table, key, and index exists.
	// This makes schema creation deterministic even when relations form cycles.
	for _, table := range model.Tables {
		for _, foreignKey := range table.ForeignKeys {
			statements = append(statements, addForeignKeyConstraint(table.Name, foreignKey))
		}
	}
	if len(statements) == 0 {
		return "\n", nil
	}
	return strings.Join(statements, "\n\n") + "\n", nil
}

func renderEnum(enum schema.EnumType) string {
	values := make([]string, len(enum.Values))
	for i, value := range enum.Values {
		values[i] = quoteLiteral(value)
	}
	return "CREATE TYPE " + quote(enum.Name) + " AS ENUM (" + strings.Join(values, ", ") + ");"
}

func renderTable(table schema.Table) (string, error) {
	if len(table.Columns) == 0 {
		return "", fmt.Errorf("postgres: table %q has no columns", table.Name)
	}

	inlinePrimaryKeys := inlinePrimaryKeyColumns(table)
	inlineSingle := table.PrimaryKey == nil && len(inlinePrimaryKeys) == 1
	definitions := make([]string, 0, len(table.Columns)+1+len(table.UniqueConstraints)+len(table.Checks))
	for _, column := range table.Columns {
		definitions = append(definitions, "    "+renderColumn(column, inlineSingle && column.PrimaryKey))
	}
	if table.PrimaryKey != nil {
		definitions = append(definitions, "    CONSTRAINT "+quote(table.PrimaryKey.Name)+" PRIMARY KEY ("+quoteList(table.PrimaryKey.Columns)+")")
	} else if len(inlinePrimaryKeys) > 1 {
		definitions = append(definitions, "    PRIMARY KEY ("+quoteList(inlinePrimaryKeys)+")")
	}
	for _, constraint := range table.UniqueConstraints {
		definitions = append(definitions, "    CONSTRAINT "+quote(constraint.Name)+" UNIQUE ("+quoteList(constraint.Columns)+")")
	}
	for _, constraint := range table.Checks {
		definitions = append(definitions, "    CONSTRAINT "+quote(constraint.Name)+" CHECK ("+constraint.Expression+")")
	}

	statements := []string{
		"CREATE TABLE " + quote(table.Name) + " (\n" + strings.Join(definitions, ",\n") + "\n);",
	}
	for _, index := range table.Indexes {
		statements = append(statements, renderIndex(table.Name, index))
	}
	return strings.Join(statements, "\n"), nil
}

func renderColumn(column schema.Column, inlinePrimaryKey bool) string {
	parts := []string{quote(column.Name), column.Type}
	if !column.Nullable {
		parts = append(parts, "NOT NULL")
	}
	if column.Generated != "" {
		parts = append(parts, "GENERATED ALWAYS AS ("+column.Generated+") STORED")
	} else if column.Default != "" {
		parts = append(parts, "DEFAULT", column.Default)
	}
	if column.Unique {
		parts = append(parts, "UNIQUE")
	}
	if inlinePrimaryKey {
		parts = append(parts, "PRIMARY KEY")
	}
	return strings.Join(parts, " ")
}

func renderIndex(table string, index schema.Index) string {
	var builder strings.Builder
	builder.WriteString("CREATE ")
	if index.Unique {
		builder.WriteString("UNIQUE ")
	}
	builder.WriteString("INDEX ")
	builder.WriteString(quote(index.Name))
	builder.WriteString(" ON ")
	builder.WriteString(quote(table))
	if index.Method != "" {
		builder.WriteString(" USING ")
		builder.WriteString(index.Method)
	}
	builder.WriteString(" (")
	if index.Expression != "" {
		builder.WriteByte('(')
		builder.WriteString(index.Expression)
		builder.WriteByte(')')
	} else {
		builder.WriteString(quoteList(index.Columns))
	}
	builder.WriteByte(')')
	if len(index.Include) > 0 {
		builder.WriteString(" INCLUDE (")
		builder.WriteString(quoteList(index.Include))
		builder.WriteByte(')')
	}
	if strings.TrimSpace(index.Predicate) != "" {
		builder.WriteString(" WHERE ")
		builder.WriteString(index.Predicate)
	}
	builder.WriteByte(';')
	return builder.String()
}

func addForeignKeyConstraint(table string, foreignKey schema.ForeignKey) string {
	var builder strings.Builder
	builder.WriteString("ALTER TABLE ")
	builder.WriteString(quote(table))
	builder.WriteString(" ADD CONSTRAINT ")
	builder.WriteString(quote(foreignKey.Name))
	builder.WriteString(" FOREIGN KEY (")
	builder.WriteString(quoteList(foreignKey.Columns))
	builder.WriteString(") REFERENCES ")
	builder.WriteString(quote(foreignKey.ReferencedTable))
	builder.WriteString(" (")
	builder.WriteString(quoteList(foreignKey.ReferencedColumns))
	builder.WriteByte(')')
	if foreignKey.OnDelete != "" {
		builder.WriteString(" ON DELETE ")
		builder.WriteString(string(foreignKey.OnDelete))
	}
	if foreignKey.OnUpdate != "" {
		builder.WriteString(" ON UPDATE ")
		builder.WriteString(string(foreignKey.OnUpdate))
	}
	if foreignKey.Deferrable {
		builder.WriteString(" DEFERRABLE")
		if foreignKey.InitiallyDeferred {
			builder.WriteString(" INITIALLY DEFERRED")
		}
	}
	builder.WriteByte(';')
	return builder.String()
}

func inlinePrimaryKeyColumns(table schema.Table) []string {
	var result []string
	for _, column := range table.Columns {
		if column.PrimaryKey {
			result = append(result, column.Name)
		}
	}
	return result
}

func quoteList(identifiers []string) string {
	result := make([]string, len(identifiers))
	for i, identifier := range identifiers {
		result[i] = quote(identifier)
	}
	return strings.Join(result, ", ")
}

func quote(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func quoteLiteral(value string) string {
	return `'` + strings.ReplaceAll(value, `'`, `''`) + `'`
}
