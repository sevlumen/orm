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
	if !column.Nullable && column.Default == "" {
		return RiskReview, fmt.Sprintf("adding non-null column %s without a default can fail on a non-empty table", column.Name)
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
