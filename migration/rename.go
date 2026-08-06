package migration

import (
	"fmt"
	"strings"

	"github.com/sevlumen/orm/schema"
)

// RenameObject is the migration operation emitted for an explicit rename intent.
const RenameObject OperationKind = "rename_object"

// RenameKind identifies the PostgreSQL schema object being renamed.
type RenameKind string

const (
	RenameTable      RenameKind = "table"
	RenameColumn     RenameKind = "column"
	RenameIndex      RenameKind = "index"
	RenameConstraint RenameKind = "constraint"
	RenameEnum       RenameKind = "enum"
)

// Rename is an ordered explicit schema rename. Table is required for column,
// index, and constraint renames. Later intents observe earlier renames.
type Rename struct {
	Kind  RenameKind
	Table string
	From  string
	To    string
}

// DiffOptions configures explicit migration intent that cannot be inferred safely.
type DiffOptions struct {
	Renames []Rename
}

// DiffWithOptions applies explicit rename intent to the before snapshot and then
// plans every remaining schema difference normally.
func DiffWithOptions(before, after Snapshot, options DiffOptions) (Plan, error) {
	if err := before.Validate(); err != nil {
		return Plan{}, fmt.Errorf("migration: before snapshot: %w", err)
	}
	if err := after.Validate(); err != nil {
		return Plan{}, fmt.Errorf("migration: after snapshot: %w", err)
	}

	working := cloneSchema(before.Schema)
	renameOperations := make([]Operation, 0, len(options.Renames))
	for i, intent := range options.Renames {
		if err := intent.Validate(); err != nil {
			return Plan{}, fmt.Errorf("migration: rename intent %d: %w", i, err)
		}
		operation, err := applyRename(&working, intent)
		if err != nil {
			return Plan{}, fmt.Errorf("migration: rename intent %d: %w", i, err)
		}
		renameOperations = append(renameOperations, operation)
	}

	transformed, err := NewSnapshot(working)
	if err != nil {
		return Plan{}, fmt.Errorf("migration: transformed before snapshot: %w", err)
	}
	remaining, err := Diff(transformed, after)
	if err != nil {
		return Plan{}, err
	}

	operations := make([]Operation, 0, len(renameOperations)+len(remaining.Operations))
	operations = append(operations, renameOperations...)
	operations = append(operations, remaining.Operations...)
	plan := Plan{Operations: operations}
	if err := plan.Validate(); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

// Validate checks rename shape and PostgreSQL identifier boundaries.
func (r Rename) Validate() error {
	switch r.Kind {
	case RenameTable, RenameEnum:
		if strings.TrimSpace(r.Table) != "" {
			return fmt.Errorf("%s rename must not set a parent table", r.Kind)
		}
	case RenameColumn, RenameIndex, RenameConstraint:
		if err := validateRenameIdentifier("table", r.Table); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported rename kind %q", r.Kind)
	}
	if err := validateRenameIdentifier("source", r.From); err != nil {
		return err
	}
	if err := validateRenameIdentifier("target", r.To); err != nil {
		return err
	}
	if r.From == r.To {
		return fmt.Errorf("rename source and target are both %q", r.From)
	}
	return nil
}

func validateRenameIdentifier(kind, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("rename %s name is required", kind)
	}
	if len(value) > 63 {
		return fmt.Errorf("rename %s name %q exceeds 63 bytes", kind, value)
	}
	if strings.ContainsRune(value, '\x00') {
		return fmt.Errorf("rename %s name %q contains NUL", kind, value)
	}
	return nil
}

func applyRename(model *schema.Schema, intent Rename) (Operation, error) {
	switch intent.Kind {
	case RenameTable:
		return applyTableRename(model, intent)
	case RenameColumn:
		return applyColumnRename(model, intent)
	case RenameIndex:
		return applyIndexRename(model, intent)
	case RenameConstraint:
		return applyConstraintRename(model, intent)
	case RenameEnum:
		return applyEnumRename(model, intent)
	default:
		return Operation{}, fmt.Errorf("unsupported rename kind %q", intent.Kind)
	}
}

func renameOperation(intent Rename, reason string) Operation {
	copy := intent
	return Operation{Kind: RenameObject, Rename: &copy, Risk: RiskReview, Reason: reason}
}

func applyTableRename(model *schema.Schema, intent Rename) (Operation, error) {
	if findTableIndex(*model, intent.To) >= 0 {
		return Operation{}, fmt.Errorf("table rename target %q already exists", intent.To)
	}
	index := findTableIndex(*model, intent.From)
	if index < 0 {
		return Operation{}, fmt.Errorf("table rename source %q does not exist", intent.From)
	}
	model.Tables[index].Name = intent.To
	for tableIndex := range model.Tables {
		for foreignKeyIndex := range model.Tables[tableIndex].ForeignKeys {
			foreignKey := &model.Tables[tableIndex].ForeignKeys[foreignKeyIndex]
			if foreignKey.ReferencedTable == intent.From {
				foreignKey.ReferencedTable = intent.To
			}
		}
	}
	return renameOperation(intent, fmt.Sprintf("renaming table %s to %s acquires an exclusive catalog lock", intent.From, intent.To)), nil
}

func applyColumnRename(model *schema.Schema, intent Rename) (Operation, error) {
	tableIndex := findTableIndex(*model, intent.Table)
	if tableIndex < 0 {
		return Operation{}, fmt.Errorf("column rename table %q does not exist", intent.Table)
	}
	table := &model.Tables[tableIndex]
	if findColumnIndex(*table, intent.To) >= 0 {
		return Operation{}, fmt.Errorf("column rename target %q.%q already exists", intent.Table, intent.To)
	}
	columnIndex := findColumnIndex(*table, intent.From)
	if columnIndex < 0 {
		return Operation{}, fmt.Errorf("column rename source %q.%q does not exist", intent.Table, intent.From)
	}
	table.Columns[columnIndex].Name = intent.To
	if table.PrimaryKey != nil {
		replaceName(table.PrimaryKey.Columns, intent.From, intent.To)
	}
	for index := range table.UniqueConstraints {
		replaceName(table.UniqueConstraints[index].Columns, intent.From, intent.To)
	}
	for index := range table.Indexes {
		replaceName(table.Indexes[index].Columns, intent.From, intent.To)
		replaceName(table.Indexes[index].Include, intent.From, intent.To)
	}
	for index := range table.ForeignKeys {
		replaceName(table.ForeignKeys[index].Columns, intent.From, intent.To)
	}
	for otherTable := range model.Tables {
		for foreignKeyIndex := range model.Tables[otherTable].ForeignKeys {
			foreignKey := &model.Tables[otherTable].ForeignKeys[foreignKeyIndex]
			if foreignKey.ReferencedTable == intent.Table {
				replaceName(foreignKey.ReferencedColumns, intent.From, intent.To)
			}
		}
	}
	return renameOperation(intent, fmt.Sprintf("renaming column %s.%s to %s updates structured dependencies without rewriting raw SQL expressions", intent.Table, intent.From, intent.To)), nil
}

func applyIndexRename(model *schema.Schema, intent Rename) (Operation, error) {
	tableIndex := findTableIndex(*model, intent.Table)
	if tableIndex < 0 {
		return Operation{}, fmt.Errorf("index rename table %q does not exist", intent.Table)
	}
	table := &model.Tables[tableIndex]
	for _, index := range table.Indexes {
		if index.Name == intent.To {
			return Operation{}, fmt.Errorf("index rename target %q already exists on table %q", intent.To, intent.Table)
		}
	}
	for index := range table.Indexes {
		if table.Indexes[index].Name == intent.From {
			table.Indexes[index].Name = intent.To
			return renameOperation(intent, fmt.Sprintf("renaming index %s to %s changes its relation name without rebuilding it", intent.From, intent.To)), nil
		}
	}
	return Operation{}, fmt.Errorf("index rename source %q does not exist on table %q", intent.From, intent.Table)
}

func applyConstraintRename(model *schema.Schema, intent Rename) (Operation, error) {
	tableIndex := findTableIndex(*model, intent.Table)
	if tableIndex < 0 {
		return Operation{}, fmt.Errorf("constraint rename table %q does not exist", intent.Table)
	}
	table := &model.Tables[tableIndex]
	if constraintExists(*table, intent.To) {
		return Operation{}, fmt.Errorf("constraint rename target %q already exists on table %q", intent.To, intent.Table)
	}
	matches := 0
	if table.PrimaryKey != nil && table.PrimaryKey.Name == intent.From {
		table.PrimaryKey.Name = intent.To
		matches++
	}
	for index := range table.UniqueConstraints {
		if table.UniqueConstraints[index].Name == intent.From {
			table.UniqueConstraints[index].Name = intent.To
			matches++
		}
	}
	for index := range table.Checks {
		if table.Checks[index].Name == intent.From {
			table.Checks[index].Name = intent.To
			matches++
		}
	}
	for index := range table.ForeignKeys {
		if table.ForeignKeys[index].Name == intent.From {
			table.ForeignKeys[index].Name = intent.To
			matches++
		}
	}
	if matches == 0 {
		return Operation{}, fmt.Errorf("constraint rename source %q does not exist on table %q", intent.From, intent.Table)
	}
	if matches > 1 {
		return Operation{}, fmt.Errorf("constraint rename source %q is ambiguous on table %q", intent.From, intent.Table)
	}
	return renameOperation(intent, fmt.Sprintf("renaming constraint %s.%s to %s preserves its backing object and data", intent.Table, intent.From, intent.To)), nil
}

func applyEnumRename(model *schema.Schema, intent Rename) (Operation, error) {
	for _, enum := range model.Enums {
		if enum.Name == intent.To {
			return Operation{}, fmt.Errorf("enum rename target %q already exists", intent.To)
		}
	}
	found := false
	for index := range model.Enums {
		if model.Enums[index].Name == intent.From {
			model.Enums[index].Name = intent.To
			found = true
			break
		}
	}
	if !found {
		return Operation{}, fmt.Errorf("enum rename source %q does not exist", intent.From)
	}
	for tableIndex := range model.Tables {
		for columnIndex := range model.Tables[tableIndex].Columns {
			column := &model.Tables[tableIndex].Columns[columnIndex]
			column.Type = renameEnumColumnType(column.Type, intent.From, intent.To)
		}
	}
	return renameOperation(intent, fmt.Sprintf("renaming enum %s to %s updates structured column types without changing enum labels", intent.From, intent.To)), nil
}

func renameEnumColumnType(columnType, from, to string) string {
	trimmed := strings.TrimSpace(columnType)
	quotedFrom := quoteTypeName(from)
	quotedTo := quoteTypeName(to)
	switch trimmed {
	case from:
		return to
	case from + "[]":
		return to + "[]"
	case quotedFrom:
		return quotedTo
	case quotedFrom + "[]":
		return quotedTo + "[]"
	default:
		return columnType
	}
}

func findTableIndex(model schema.Schema, name string) int {
	for index := range model.Tables {
		if model.Tables[index].Name == name {
			return index
		}
	}
	return -1
}

func findColumnIndex(table schema.Table, name string) int {
	for index := range table.Columns {
		if table.Columns[index].Name == name {
			return index
		}
	}
	return -1
}

func constraintExists(table schema.Table, name string) bool {
	if table.PrimaryKey != nil && table.PrimaryKey.Name == name {
		return true
	}
	for _, constraint := range table.UniqueConstraints {
		if constraint.Name == name {
			return true
		}
	}
	for _, constraint := range table.Checks {
		if constraint.Name == name {
			return true
		}
	}
	for _, constraint := range table.ForeignKeys {
		if constraint.Name == name {
			return true
		}
	}
	return false
}

func replaceName(values []string, from, to string) {
	for index := range values {
		if values[index] == from {
			values[index] = to
		}
	}
}
