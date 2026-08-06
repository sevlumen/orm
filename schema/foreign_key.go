package schema

import (
	"fmt"
	"strings"
)

// ReferentialAction is a PostgreSQL foreign-key update/delete action.
type ReferentialAction string

const (
	NoAction   ReferentialAction = "NO ACTION"
	Restrict   ReferentialAction = "RESTRICT"
	Cascade    ReferentialAction = "CASCADE"
	SetNull    ReferentialAction = "SET NULL"
	SetDefault ReferentialAction = "SET DEFAULT"
)

// ForeignKey describes a named single-column or composite foreign key.
type ForeignKey struct {
	Name              string            `json:"name"`
	Columns           []string          `json:"columns"`
	ReferencedTable   string            `json:"referencedTable"`
	ReferencedColumns []string          `json:"referencedColumns"`
	OnDelete          ReferentialAction `json:"onDelete,omitempty"`
	OnUpdate          ReferentialAction `json:"onUpdate,omitempty"`
	Deferrable        bool              `json:"deferrable,omitempty"`
	InitiallyDeferred bool              `json:"initiallyDeferred,omitempty"`
}

func validateForeignKeyShape(table string, foreignKey ForeignKey, available map[string]Column) error {
	if err := validateIdentifier("foreign key", foreignKey.Name); err != nil {
		return fmt.Errorf("schema: table %q: %w", table, err)
	}
	if err := validateIdentifier("referenced table", foreignKey.ReferencedTable); err != nil {
		return fmt.Errorf("schema: foreign key %q on table %q: %w", foreignKey.Name, table, err)
	}
	if len(foreignKey.Columns) == 0 {
		return fmt.Errorf("schema: foreign key %q on table %q has no local columns", foreignKey.Name, table)
	}
	if len(foreignKey.Columns) != len(foreignKey.ReferencedColumns) {
		return fmt.Errorf("schema: foreign key %q on table %q has %d local columns but %d referenced columns", foreignKey.Name, table, len(foreignKey.Columns), len(foreignKey.ReferencedColumns))
	}
	seen := map[string]struct{}{}
	for _, columnName := range foreignKey.Columns {
		if _, exists := available[columnName]; !exists {
			return fmt.Errorf("schema: foreign key %q on table %q references unknown local column %q", foreignKey.Name, table, columnName)
		}
		if _, duplicate := seen[columnName]; duplicate {
			return fmt.Errorf("schema: foreign key %q on table %q repeats local column %q", foreignKey.Name, table, columnName)
		}
		seen[columnName] = struct{}{}
	}
	if err := validateReferentialAction(foreignKey.Name, table, "ON DELETE", foreignKey.OnDelete); err != nil {
		return err
	}
	if err := validateReferentialAction(foreignKey.Name, table, "ON UPDATE", foreignKey.OnUpdate); err != nil {
		return err
	}
	if foreignKey.InitiallyDeferred && !foreignKey.Deferrable {
		return fmt.Errorf("schema: foreign key %q on table %q is initially deferred but not deferrable", foreignKey.Name, table)
	}
	return nil
}

func validateReferentialAction(name, table, clause string, action ReferentialAction) error {
	switch action {
	case "", NoAction, Restrict, Cascade, SetNull, SetDefault:
		return nil
	default:
		return fmt.Errorf("schema: foreign key %q on table %q has unsupported %s action %q", name, table, clause, action)
	}
}

func validateForeignKeyReferences(tableList []Table, tables map[string]Table) error {
	for _, table := range tableList {
		for _, foreignKey := range table.ForeignKeys {
			if err := validateForeignKeyReference(table, foreignKey, tables); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateForeignKeyReference(table Table, foreignKey ForeignKey, tables map[string]Table) error {
	referenced, exists := tables[foreignKey.ReferencedTable]
	if !exists {
		return fmt.Errorf("schema: foreign key %q on table %q references unknown table %q", foreignKey.Name, table.Name, foreignKey.ReferencedTable)
	}

	referencedColumns := make(map[string]Column, len(referenced.Columns))
	for _, column := range referenced.Columns {
		referencedColumns[column.Name] = column
	}
	seenReferenced := map[string]struct{}{}
	for _, columnName := range foreignKey.ReferencedColumns {
		if _, exists := referencedColumns[columnName]; !exists {
			return fmt.Errorf("schema: foreign key %q on table %q references unknown column %q.%q", foreignKey.Name, table.Name, referenced.Name, columnName)
		}
		if _, duplicate := seenReferenced[columnName]; duplicate {
			return fmt.Errorf("schema: foreign key %q on table %q repeats referenced column %q", foreignKey.Name, table.Name, columnName)
		}
		seenReferenced[columnName] = struct{}{}
	}
	if !tableHasUniqueKey(referenced, foreignKey.ReferencedColumns) {
		return fmt.Errorf("schema: foreign key %q on table %q references columns %v on table %q without a matching primary or unique key", foreignKey.Name, table.Name, foreignKey.ReferencedColumns, referenced.Name)
	}

	localColumns := make(map[string]Column, len(table.Columns))
	for _, column := range table.Columns {
		localColumns[column.Name] = column
	}
	for i, columnName := range foreignKey.Columns {
		column := localColumns[columnName]
		referencedColumn := referencedColumns[foreignKey.ReferencedColumns[i]]
		if column.Type != referencedColumn.Type {
			return fmt.Errorf("schema: foreign key %q on table %q has incompatible column types %q.%q=%s and %q.%q=%s", foreignKey.Name, table.Name, table.Name, columnName, column.Type, referenced.Name, foreignKey.ReferencedColumns[i], referencedColumn.Type)
		}
		if foreignKey.OnDelete == SetNull || foreignKey.OnUpdate == SetNull {
			if !column.Nullable {
				return fmt.Errorf("schema: foreign key %q on table %q uses SET NULL with non-nullable column %q", foreignKey.Name, table.Name, columnName)
			}
		}
		if foreignKey.OnDelete == SetDefault || foreignKey.OnUpdate == SetDefault {
			if !column.Nullable && strings.TrimSpace(column.Default) == "" {
				return fmt.Errorf("schema: foreign key %q on table %q uses SET DEFAULT with non-nullable column %q that has no default", foreignKey.Name, table.Name, columnName)
			}
		}
	}
	return nil
}

func tableHasUniqueKey(table Table, columns []string) bool {
	if table.PrimaryKey != nil && stringSlicesEqual(table.PrimaryKey.Columns, columns) {
		return true
	}
	if table.PrimaryKey == nil {
		var inlinePrimary []string
		for _, column := range table.Columns {
			if column.PrimaryKey {
				inlinePrimary = append(inlinePrimary, column.Name)
			}
		}
		if len(inlinePrimary) > 0 && stringSlicesEqual(inlinePrimary, columns) {
			return true
		}
	}
	for _, unique := range table.UniqueConstraints {
		if stringSlicesEqual(unique.Columns, columns) {
			return true
		}
	}
	if len(columns) == 1 {
		for _, column := range table.Columns {
			if column.Name == columns[0] && column.Unique {
				return true
			}
		}
	}
	for _, index := range table.Indexes {
		if index.Unique && index.Expression == "" && strings.TrimSpace(index.Predicate) == "" && stringSlicesEqual(index.Columns, columns) {
			return true
		}
	}
	return false
}
