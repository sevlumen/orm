package migration

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sevlumen/orm/schema"
)

// Risk describes the operational risk of a generated migration operation.
type Risk uint8

const (
	RiskSafe Risk = iota
	RiskReview
	RiskDestructive
)

func (r Risk) String() string {
	switch r {
	case RiskSafe:
		return "safe"
	case RiskReview:
		return "review"
	case RiskDestructive:
		return "destructive"
	default:
		return "unknown"
	}
}

// OperationKind identifies a schema change.
type OperationKind string

const (
	CreateTable OperationKind = "create_table"
	DropTable   OperationKind = "drop_table"
	AddColumn   OperationKind = "add_column"
	DropColumn  OperationKind = "drop_column"
	AlterColumn OperationKind = "alter_column"
)

// Operation contains enough before/after state to render both up and down SQL.
type Operation struct {
	Kind         OperationKind
	Table        string
	BeforeTable  *schema.Table
	AfterTable   *schema.Table
	BeforeColumn *schema.Column
	AfterColumn  *schema.Column
	Risk         Risk
	Reason       string
}

// Plan is a deterministic ordered set of migration operations.
type Plan struct {
	Operations []Operation
}

// Validate checks that a plan is safe to pass to a renderer without panics.
func (p Plan) Validate() error {
	for i, operation := range p.Operations {
		if strings.TrimSpace(operation.Table) == "" {
			return fmt.Errorf("migration: operation %d has no table name", i)
		}
		if operation.Risk > RiskDestructive {
			return fmt.Errorf("migration: operation %d has invalid risk %d", i, operation.Risk)
		}
		switch operation.Kind {
		case CreateTable:
			if operation.AfterTable == nil {
				return fmt.Errorf("migration: create_table operation %d has no after table", i)
			}
			if operation.AfterTable.Name != operation.Table {
				return fmt.Errorf("migration: create_table operation %d table name mismatch", i)
			}
			if err := (schema.Schema{Tables: []schema.Table{*operation.AfterTable}}).Validate(); err != nil {
				return fmt.Errorf("migration: create_table operation %d: %w", i, err)
			}
		case DropTable:
			if operation.BeforeTable == nil {
				return fmt.Errorf("migration: drop_table operation %d has no before table", i)
			}
			if operation.BeforeTable.Name != operation.Table {
				return fmt.Errorf("migration: drop_table operation %d table name mismatch", i)
			}
		case AddColumn:
			if operation.AfterColumn == nil {
				return fmt.Errorf("migration: add_column operation %d has no after column", i)
			}
			if err := validateOperationColumn(operation.Table, *operation.AfterColumn); err != nil {
				return fmt.Errorf("migration: add_column operation %d: %w", i, err)
			}
		case DropColumn:
			if operation.BeforeColumn == nil {
				return fmt.Errorf("migration: drop_column operation %d has no before column", i)
			}
			if err := validateOperationColumn(operation.Table, *operation.BeforeColumn); err != nil {
				return fmt.Errorf("migration: drop_column operation %d: %w", i, err)
			}
		case AlterColumn:
			if operation.BeforeColumn == nil || operation.AfterColumn == nil {
				return fmt.Errorf("migration: alter_column operation %d requires before and after columns", i)
			}
			if operation.BeforeColumn.Name != operation.AfterColumn.Name {
				return fmt.Errorf("migration: alter_column operation %d cannot rename columns", i)
			}
			if err := validateOperationColumn(operation.Table, *operation.BeforeColumn); err != nil {
				return fmt.Errorf("migration: alter_column operation %d before column: %w", i, err)
			}
			if err := validateOperationColumn(operation.Table, *operation.AfterColumn); err != nil {
				return fmt.Errorf("migration: alter_column operation %d after column: %w", i, err)
			}
		default:
			return fmt.Errorf("migration: operation %d has unsupported kind %q", i, operation.Kind)
		}
	}
	return nil
}

func validateOperationColumn(table string, column schema.Column) error {
	model := schema.Schema{Tables: []schema.Table{{Name: table, Columns: []schema.Column{column}}}}
	return model.Validate()
}

// MaxRisk returns the highest risk present in the plan.
func (p Plan) MaxRisk() Risk {
	result := RiskSafe
	for _, operation := range p.Operations {
		if operation.Risk > result {
			result = operation.Risk
		}
	}
	return result
}

// Warnings returns review and destructive operation explanations.
func (p Plan) Warnings() []string {
	var warnings []string
	for _, operation := range p.Operations {
		if operation.Risk != RiskSafe {
			warnings = append(warnings, fmt.Sprintf("%s %s: %s", operation.Risk, operation.Kind, operation.Reason))
		}
	}
	return warnings
}

// Diff compares two validated snapshots and produces a deterministic plan.
func Diff(before, after Snapshot) (Plan, error) {
	if err := before.Validate(); err != nil {
		return Plan{}, fmt.Errorf("migration: before snapshot: %w", err)
	}
	if err := after.Validate(); err != nil {
		return Plan{}, fmt.Errorf("migration: after snapshot: %w", err)
	}

	beforeTables := tableMap(before.Schema)
	afterTables := tableMap(after.Schema)
	var creates, adds, alters, drops, dropTables []Operation

	for _, name := range sortedTableNames(afterTables) {
		afterTable := afterTables[name]
		beforeTable, exists := beforeTables[name]
		if !exists {
			copy := afterTable
			creates = append(creates, Operation{Kind: CreateTable, Table: name, AfterTable: &copy})
			continue
		}

		beforeColumns := columnMap(beforeTable)
		afterColumns := columnMap(afterTable)
		for _, columnName := range sortedColumnNames(afterColumns) {
			afterColumn := afterColumns[columnName]
			beforeColumn, exists := beforeColumns[columnName]
			if !exists {
				copy := afterColumn
				risk, reason := addColumnRisk(afterColumn)
				adds = append(adds, Operation{Kind: AddColumn, Table: name, AfterColumn: &copy, Risk: risk, Reason: reason})
				continue
			}
			if columnsEqual(beforeColumn, afterColumn) {
				continue
			}
			if beforeColumn.PrimaryKey != afterColumn.PrimaryKey || beforeColumn.Unique != afterColumn.Unique {
				return Plan{}, fmt.Errorf("migration: %s.%s changes primaryKey or unique; write an explicit migration", name, columnName)
			}
			beforeCopy, afterCopy := beforeColumn, afterColumn
			risk, reason := alterColumnRisk(beforeColumn, afterColumn)
			alters = append(alters, Operation{Kind: AlterColumn, Table: name, BeforeColumn: &beforeCopy, AfterColumn: &afterCopy, Risk: risk, Reason: reason})
		}
		for _, columnName := range sortedColumnNames(beforeColumns) {
			if _, exists := afterColumns[columnName]; exists {
				continue
			}
			copy := beforeColumns[columnName]
			drops = append(drops, Operation{Kind: DropColumn, Table: name, BeforeColumn: &copy, Risk: RiskDestructive, Reason: fmt.Sprintf("dropping %s.%s loses column data", name, columnName)})
		}
	}

	for _, name := range sortedTableNames(beforeTables) {
		if _, exists := afterTables[name]; exists {
			continue
		}
		copy := beforeTables[name]
		dropTables = append(dropTables, Operation{Kind: DropTable, Table: name, BeforeTable: &copy, Risk: RiskDestructive, Reason: fmt.Sprintf("dropping table %s loses table data", name)})
	}

	operations := make([]Operation, 0, len(creates)+len(adds)+len(alters)+len(drops)+len(dropTables))
	operations = append(operations, creates...)
	operations = append(operations, adds...)
	operations = append(operations, alters...)
	operations = append(operations, drops...)
	operations = append(operations, dropTables...)
	return Plan{Operations: operations}, nil
}

func addColumnRisk(column schema.Column) (Risk, string) {
	var reasons []string
	if !column.Nullable && column.Default == "" {
		reasons = append(reasons, fmt.Sprintf("adding non-null column %s without a default can fail on a non-empty table", column.Name))
	}
	if column.Unique {
		reasons = append(reasons, fmt.Sprintf("adding unique column %s can fail when existing rows produce duplicate values", column.Name))
	}
	if column.PrimaryKey {
		reasons = append(reasons, fmt.Sprintf("adding primary-key column %s requires valid values for every existing row", column.Name))
	}
	if len(reasons) > 0 {
		return RiskReview, strings.Join(reasons, "; ")
	}
	return RiskSafe, ""
}

func alterColumnRisk(before, after schema.Column) (Risk, string) {
	var reasons []string
	risk := RiskSafe
	if before.Type != after.Type {
		risk = RiskReview
		reasons = append(reasons, fmt.Sprintf("type changes from %s to %s and may require an explicit USING expression", before.Type, after.Type))
	}
	if before.Nullable && !after.Nullable {
		risk = RiskReview
		reasons = append(reasons, "SET NOT NULL can fail while NULL rows exist")
	}
	return risk, strings.Join(reasons, "; ")
}

func columnsEqual(a, b schema.Column) bool {
	return a.Name == b.Name && a.Type == b.Type && a.Nullable == b.Nullable &&
		a.PrimaryKey == b.PrimaryKey && a.Unique == b.Unique && a.Default == b.Default
}

func tableMap(model schema.Schema) map[string]schema.Table {
	result := make(map[string]schema.Table, len(model.Tables))
	for _, table := range model.Tables {
		result[table.Name] = table
	}
	return result
}

func columnMap(table schema.Table) map[string]schema.Column {
	result := make(map[string]schema.Column, len(table.Columns))
	for _, column := range table.Columns {
		result[column.Name] = column
	}
	return result
}

func sortedTableNames(tables map[string]schema.Table) []string {
	names := make([]string, 0, len(tables))
	for name := range tables {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortedColumnNames(columns map[string]schema.Column) []string {
	names := make([]string, 0, len(columns))
	for name := range columns {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
