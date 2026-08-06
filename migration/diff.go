package migration

import (
	"fmt"
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
	CreateExtension      OperationKind = "create_extension"
	CreateEnum           OperationKind = "create_enum"
	DropEnum             OperationKind = "drop_enum"
	CreateTable          OperationKind = "create_table"
	DropTable            OperationKind = "drop_table"
	AddColumn            OperationKind = "add_column"
	DropColumn           OperationKind = "drop_column"
	AlterColumn          OperationKind = "alter_column"
	CreateIndex          OperationKind = "create_index"
	DropIndex            OperationKind = "drop_index"
	AddUniqueConstraint  OperationKind = "add_unique_constraint"
	DropUniqueConstraint OperationKind = "drop_unique_constraint"
	AddCheckConstraint   OperationKind = "add_check_constraint"
	DropCheckConstraint  OperationKind = "drop_check_constraint"
	AddForeignKey        OperationKind = "add_foreign_key"
	DropForeignKey       OperationKind = "drop_foreign_key"
)

// Operation contains enough before/after state to render both up and down SQL.
type Operation struct {
	Kind             OperationKind
	Table            string
	BeforeExtension  *schema.Extension
	AfterExtension   *schema.Extension
	BeforeEnum       *schema.EnumType
	AfterEnum        *schema.EnumType
	BeforeTable      *schema.Table
	AfterTable       *schema.Table
	BeforeColumn     *schema.Column
	AfterColumn      *schema.Column
	BeforeIndex      *schema.Index
	AfterIndex       *schema.Index
	BeforeUnique     *schema.UniqueConstraint
	AfterUnique      *schema.UniqueConstraint
	BeforeCheck      *schema.CheckConstraint
	AfterCheck       *schema.CheckConstraint
	BeforeForeignKey *schema.ForeignKey
	AfterForeignKey  *schema.ForeignKey
	Risk             Risk
	Reason           string
}

// Plan is a deterministic ordered set of migration operations.
type Plan struct {
	Operations []Operation
}

// Validate checks that a plan is safe to pass to a renderer without panics.
func (p Plan) Validate() error {
	for i, operation := range p.Operations {
		if operation.Risk > RiskDestructive {
			return fmt.Errorf("migration: operation %d has invalid risk %d", i, operation.Risk)
		}
		if err := validateOperation(operation); err != nil {
			return fmt.Errorf("migration: operation %d (%s): %w", i, operation.Kind, err)
		}
	}
	return nil
}

func validateOperation(operation Operation) error {
	requireTable := func() error {
		if strings.TrimSpace(operation.Table) == "" {
			return fmt.Errorf("table name is required")
		}
		return nil
	}

	switch operation.Kind {
	case CreateExtension:
		if operation.AfterExtension == nil {
			return fmt.Errorf("after extension is required")
		}
		return validateNativeOperation(schema.Schema{Extensions: []schema.Extension{*operation.AfterExtension}})
	case CreateEnum:
		if operation.AfterEnum == nil {
			return fmt.Errorf("after enum is required")
		}
		return validateNativeOperation(schema.Schema{Enums: []schema.EnumType{cloneEnum(*operation.AfterEnum)}})
	case DropEnum:
		if operation.BeforeEnum == nil {
			return fmt.Errorf("before enum is required")
		}
		return validateNativeOperation(schema.Schema{Enums: []schema.EnumType{cloneEnum(*operation.BeforeEnum)}})
	case CreateTable:
		if err := requireTable(); err != nil {
			return err
		}
		if operation.AfterTable == nil {
			return fmt.Errorf("after table is required")
		}
		return validateOperationTable(operation.Table, *operation.AfterTable)
	case DropTable:
		if err := requireTable(); err != nil {
			return err
		}
		if operation.BeforeTable == nil {
			return fmt.Errorf("before table is required")
		}
		return validateOperationTable(operation.Table, *operation.BeforeTable)
	case AddColumn:
		if err := requireTable(); err != nil {
			return err
		}
		if operation.AfterColumn == nil {
			return fmt.Errorf("after column is required")
		}
		return validateOperationColumn(operation.Table, *operation.AfterColumn)
	case DropColumn:
		if err := requireTable(); err != nil {
			return err
		}
		if operation.BeforeColumn == nil {
			return fmt.Errorf("before column is required")
		}
		return validateOperationColumn(operation.Table, *operation.BeforeColumn)
	case AlterColumn:
		if err := requireTable(); err != nil {
			return err
		}
		if operation.BeforeColumn == nil || operation.AfterColumn == nil {
			return fmt.Errorf("before and after columns are required")
		}
		if operation.BeforeColumn.Name != operation.AfterColumn.Name {
			return fmt.Errorf("column rename requires an explicit migration")
		}
		if operation.BeforeColumn.Generated != operation.AfterColumn.Generated {
			return fmt.Errorf("generated-column expression changes require an explicit migration")
		}
		if err := validateOperationColumn(operation.Table, *operation.BeforeColumn); err != nil {
			return fmt.Errorf("before column: %w", err)
		}
		return validateOperationColumn(operation.Table, *operation.AfterColumn)
	case CreateIndex, DropIndex, AddUniqueConstraint, DropUniqueConstraint,
		AddCheckConstraint, DropCheckConstraint, AddForeignKey, DropForeignKey:
		if err := requireTable(); err != nil {
			return err
		}
		return validateMetadataOperation(operation)
	default:
		return fmt.Errorf("unsupported kind %q", operation.Kind)
	}
}

func validateNativeOperation(model schema.Schema) error {
	if err := model.Validate(); err != nil {
		return err
	}
	return nil
}

func validateOperationTable(name string, table schema.Table) error {
	if table.Name != name {
		return fmt.Errorf("table name mismatch: operation=%q value=%q", name, table.Name)
	}
	if len(table.ForeignKeys) > 0 {
		return fmt.Errorf("foreign keys require separate migration operations")
	}
	return (schema.Schema{Tables: []schema.Table{table}}).Validate()
}

func validateOperationColumn(table string, column schema.Column) error {
	return (schema.Schema{Tables: []schema.Table{{Name: table, Columns: []schema.Column{column}}}}).Validate()
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

	nativeBefore, nativeAfter, err := diffNativeObjects(before.Schema, after.Schema)
	if err != nil {
		return Plan{}, err
	}
	beforeTables := tableMap(before.Schema)
	afterTables := tableMap(after.Schema)
	dropForeignKeys, addForeignKeys := diffForeignKeys(before.Schema, after.Schema)
	var createTables, dropDependencies, addColumns, alterColumns, dropColumns, addDependencies, dropTables []Operation

	for _, tableName := range sortedKeys(afterTables) {
		afterTable := afterTables[tableName]
		beforeTable, exists := beforeTables[tableName]
		if !exists {
			copy := cloneTable(afterTable)
			copy.ForeignKeys = nil
			createTables = append(createTables, Operation{Kind: CreateTable, Table: tableName, AfterTable: &copy})
			continue
		}
		if !primaryKeysEqual(effectivePrimaryKey(beforeTable), effectivePrimaryKey(afterTable)) {
			return Plan{}, fmt.Errorf("migration: table %s changes its primary key; write an explicit migration", tableName)
		}

		dropIndexOps, addIndexOps := diffIndexes(tableName, beforeTable, afterTable)
		dropDependencies = append(dropDependencies, dropIndexOps...)
		addDependencies = append(addDependencies, addIndexOps...)
		dropUniqueOps, addUniqueOps := diffUniques(tableName, beforeTable, afterTable)
		dropDependencies = append(dropDependencies, dropUniqueOps...)
		addDependencies = append(addDependencies, addUniqueOps...)
		dropCheckOps, addCheckOps := diffChecks(tableName, beforeTable, afterTable)
		dropDependencies = append(dropDependencies, dropCheckOps...)
		addDependencies = append(addDependencies, addCheckOps...)

		beforeColumns := columnMap(beforeTable)
		afterColumns := columnMap(afterTable)
		for _, columnName := range sortedKeys(afterColumns) {
			afterColumn := afterColumns[columnName]
			beforeColumn, exists := beforeColumns[columnName]
			if !exists {
				copy := afterColumn
				risk, reason := addColumnRisk(afterColumn)
				addColumns = append(addColumns, Operation{Kind: AddColumn, Table: tableName, AfterColumn: &copy, Risk: risk, Reason: reason})
				continue
			}
			if columnsEqual(beforeColumn, afterColumn) {
				continue
			}
			if beforeColumn.PrimaryKey != afterColumn.PrimaryKey || beforeColumn.Unique != afterColumn.Unique {
				return Plan{}, fmt.Errorf("migration: %s.%s changes inline primaryKey or unique; use a named table constraint or explicit migration", tableName, columnName)
			}
			if beforeColumn.Generated != afterColumn.Generated {
				return Plan{}, fmt.Errorf("migration: %s.%s changes its generated expression; write an explicit migration", tableName, columnName)
			}
			beforeCopy, afterCopy := beforeColumn, afterColumn
			risk, reason := alterColumnRisk(beforeColumn, afterColumn)
			alterColumns = append(alterColumns, Operation{Kind: AlterColumn, Table: tableName, BeforeColumn: &beforeCopy, AfterColumn: &afterCopy, Risk: risk, Reason: reason})
		}
		for _, columnName := range sortedKeys(beforeColumns) {
			if _, exists := afterColumns[columnName]; exists {
				continue
			}
			copy := beforeColumns[columnName]
			dropColumns = append(dropColumns, Operation{Kind: DropColumn, Table: tableName, BeforeColumn: &copy, Risk: RiskDestructive, Reason: fmt.Sprintf("dropping %s.%s loses column data", tableName, columnName)})
		}
	}

	for _, tableName := range sortedKeys(beforeTables) {
		if _, exists := afterTables[tableName]; exists {
			continue
		}
		copy := cloneTable(beforeTables[tableName])
		copy.ForeignKeys = nil
		dropTables = append(dropTables, Operation{Kind: DropTable, Table: tableName, BeforeTable: &copy, Risk: RiskDestructive, Reason: fmt.Sprintf("dropping table %s loses table data", tableName)})
	}

	operations := make([]Operation, 0)
	operations = append(operations, nativeBefore...)
	operations = append(operations, dropForeignKeys...)
	operations = append(operations, dropDependencies...)
	operations = append(operations, dropTables...)
	operations = append(operations, createTables...)
	operations = append(operations, addColumns...)
	operations = append(operations, alterColumns...)
	operations = append(operations, dropColumns...)
	operations = append(operations, addDependencies...)
	operations = append(operations, addForeignKeys...)
	operations = append(operations, nativeAfter...)
	plan := Plan{Operations: operations}
	if err := plan.Validate(); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func addColumnRisk(column schema.Column) (Risk, string) {
	var reasons []string
	if !column.Nullable && column.Default == "" && column.Generated == "" {
		reasons = append(reasons, fmt.Sprintf("adding non-null column %s without a default can fail on a non-empty table", column.Name))
	}
	if column.Generated != "" {
		reasons = append(reasons, fmt.Sprintf("adding generated column %s computes values for existing rows", column.Name))
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
	if before.Type != after.Type {
		reasons = append(reasons, fmt.Sprintf("type changes from %s to %s and may require an explicit USING expression", before.Type, after.Type))
	}
	if before.Nullable && !after.Nullable {
		reasons = append(reasons, "SET NOT NULL can fail while NULL rows exist")
	}
	if len(reasons) > 0 {
		return RiskReview, strings.Join(reasons, "; ")
	}
	return RiskSafe, ""
}
