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
)

// Operation contains enough before/after state to render both up and down SQL.
type Operation struct {
	Kind         OperationKind
	Table        string
	BeforeTable  *schema.Table
	AfterTable   *schema.Table
	BeforeColumn *schema.Column
	AfterColumn  *schema.Column
	BeforeIndex  *schema.Index
	AfterIndex   *schema.Index
	BeforeUnique *schema.UniqueConstraint
	AfterUnique  *schema.UniqueConstraint
	BeforeCheck  *schema.CheckConstraint
	AfterCheck   *schema.CheckConstraint
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
		if err := validateOperation(operation); err != nil {
			return fmt.Errorf("migration: operation %d (%s): %w", i, operation.Kind, err)
		}
	}
	return nil
}

func validateOperation(operation Operation) error {
	switch operation.Kind {
	case CreateTable:
		if operation.AfterTable == nil {
			return fmt.Errorf("after table is required")
		}
		return validateOperationTable(operation.Table, *operation.AfterTable)
	case DropTable:
		if operation.BeforeTable == nil {
			return fmt.Errorf("before table is required")
		}
		return validateOperationTable(operation.Table, *operation.BeforeTable)
	case AddColumn:
		if operation.AfterColumn == nil {
			return fmt.Errorf("after column is required")
		}
		return validateOperationColumn(operation.Table, *operation.AfterColumn)
	case DropColumn:
		if operation.BeforeColumn == nil {
			return fmt.Errorf("before column is required")
		}
		return validateOperationColumn(operation.Table, *operation.BeforeColumn)
	case AlterColumn:
		if operation.BeforeColumn == nil || operation.AfterColumn == nil {
			return fmt.Errorf("before and after columns are required")
		}
		if operation.BeforeColumn.Name != operation.AfterColumn.Name {
			return fmt.Errorf("column rename requires an explicit migration")
		}
		if err := validateOperationColumn(operation.Table, *operation.BeforeColumn); err != nil {
			return fmt.Errorf("before column: %w", err)
		}
		return validateOperationColumn(operation.Table, *operation.AfterColumn)
	case CreateIndex:
		if operation.AfterIndex == nil {
			return fmt.Errorf("after index is required")
		}
		return validateOperationIndex(operation.Table, *operation.AfterIndex)
	case DropIndex:
		if operation.BeforeIndex == nil {
			return fmt.Errorf("before index is required")
		}
		return validateOperationIndex(operation.Table, *operation.BeforeIndex)
	case AddUniqueConstraint:
		if operation.AfterUnique == nil {
			return fmt.Errorf("after unique constraint is required")
		}
		return validateOperationUnique(operation.Table, *operation.AfterUnique)
	case DropUniqueConstraint:
		if operation.BeforeUnique == nil {
			return fmt.Errorf("before unique constraint is required")
		}
		return validateOperationUnique(operation.Table, *operation.BeforeUnique)
	case AddCheckConstraint:
		if operation.AfterCheck == nil {
			return fmt.Errorf("after check constraint is required")
		}
		return validateOperationCheck(operation.Table, *operation.AfterCheck)
	case DropCheckConstraint:
		if operation.BeforeCheck == nil {
			return fmt.Errorf("before check constraint is required")
		}
		return validateOperationCheck(operation.Table, *operation.BeforeCheck)
	default:
		return fmt.Errorf("unsupported kind %q", operation.Kind)
	}
}

func validateOperationTable(name string, table schema.Table) error {
	if table.Name != name {
		return fmt.Errorf("table name mismatch: operation=%q value=%q", name, table.Name)
	}
	return (schema.Schema{Tables: []schema.Table{table}}).Validate()
}

func validateOperationColumn(table string, column schema.Column) error {
	return (schema.Schema{Tables: []schema.Table{{Name: table, Columns: []schema.Column{column}}}}).Validate()
}

func validateOperationIndex(table string, index schema.Index) error {
	columnNames := append([]string(nil), index.Columns...)
	columnNames = append(columnNames, index.Include...)
	if len(columnNames) == 0 {
		columnNames = []string{"placeholder"}
	}
	columns := make([]schema.Column, 0, len(columnNames))
	seen := map[string]struct{}{}
	for _, name := range columnNames {
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		columns = append(columns, schema.Column{Name: name, Type: "text"})
	}
	return (schema.Schema{Tables: []schema.Table{{Name: table, Columns: columns, Indexes: []schema.Index{index}}}}).Validate()
}

func validateOperationUnique(table string, constraint schema.UniqueConstraint) error {
	columns := make([]schema.Column, len(constraint.Columns))
	for i, name := range constraint.Columns {
		columns[i] = schema.Column{Name: name, Type: "text"}
	}
	return (schema.Schema{Tables: []schema.Table{{Name: table, Columns: columns, UniqueConstraints: []schema.UniqueConstraint{constraint}}}}).Validate()
}

func validateOperationCheck(table string, constraint schema.CheckConstraint) error {
	return (schema.Schema{Tables: []schema.Table{{Name: table, Columns: []schema.Column{{Name: "placeholder", Type: "text"}}, Checks: []schema.CheckConstraint{constraint}}}}).Validate()
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
	var createTables, dropDependencies, addColumns, alterColumns, dropColumns, addDependencies, dropTables []Operation

	for _, tableName := range sortedKeys(afterTables) {
		afterTable := afterTables[tableName]
		beforeTable, exists := beforeTables[tableName]
		if !exists {
			copy := cloneTable(afterTable)
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
		dropTables = append(dropTables, Operation{Kind: DropTable, Table: tableName, BeforeTable: &copy, Risk: RiskDestructive, Reason: fmt.Sprintf("dropping table %s loses table data", tableName)})
	}

	operations := make([]Operation, 0, len(createTables)+len(dropDependencies)+len(addColumns)+len(alterColumns)+len(dropColumns)+len(addDependencies)+len(dropTables))
	operations = append(operations, createTables...)
	operations = append(operations, dropDependencies...)
	operations = append(operations, addColumns...)
	operations = append(operations, alterColumns...)
	operations = append(operations, dropColumns...)
	operations = append(operations, addDependencies...)
	operations = append(operations, dropTables...)
	plan := Plan{Operations: operations}
	if err := plan.Validate(); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func diffIndexes(table string, before, after schema.Table) ([]Operation, []Operation) {
	beforeMap, afterMap := indexMap(before), indexMap(after)
	var drops, adds []Operation
	for _, name := range unionKeys(beforeMap, afterMap) {
		old, oldOK := beforeMap[name]
		newValue, newOK := afterMap[name]
		changed := oldOK && newOK && !indexesEqual(old, newValue)
		if oldOK && (!newOK || changed) {
			copy := cloneIndex(old)
			drops = append(drops, Operation{Kind: DropIndex, Table: table, BeforeIndex: &copy, Risk: RiskReview, Reason: fmt.Sprintf("dropping index %s can affect query performance", name)})
		}
		if newOK && (!oldOK || changed) {
			copy := cloneIndex(newValue)
			adds = append(adds, Operation{Kind: CreateIndex, Table: table, AfterIndex: &copy, Risk: RiskReview, Reason: fmt.Sprintf("creating index %s can lock or scan existing table data", name)})
		}
	}
	return drops, adds
}

func diffUniques(table string, before, after schema.Table) ([]Operation, []Operation) {
	beforeMap, afterMap := uniqueMap(before), uniqueMap(after)
	var drops, adds []Operation
	for _, name := range unionKeys(beforeMap, afterMap) {
		old, oldOK := beforeMap[name]
		newValue, newOK := afterMap[name]
		changed := oldOK && newOK && !uniqueConstraintsEqual(old, newValue)
		if oldOK && (!newOK || changed) {
			copy := cloneUnique(old)
			drops = append(drops, Operation{Kind: DropUniqueConstraint, Table: table, BeforeUnique: &copy, Risk: RiskReview, Reason: fmt.Sprintf("dropping unique constraint %s weakens data integrity", name)})
		}
		if newOK && (!oldOK || changed) {
			copy := cloneUnique(newValue)
			adds = append(adds, Operation{Kind: AddUniqueConstraint, Table: table, AfterUnique: &copy, Risk: RiskReview, Reason: fmt.Sprintf("adding unique constraint %s can fail when duplicate rows exist", name)})
		}
	}
	return drops, adds
}

func diffChecks(table string, before, after schema.Table) ([]Operation, []Operation) {
	beforeMap, afterMap := checkMap(before), checkMap(after)
	var drops, adds []Operation
	for _, name := range unionKeys(beforeMap, afterMap) {
		old, oldOK := beforeMap[name]
		newValue, newOK := afterMap[name]
		changed := oldOK && newOK && old != newValue
		if oldOK && (!newOK || changed) {
			copy := old
			drops = append(drops, Operation{Kind: DropCheckConstraint, Table: table, BeforeCheck: &copy, Risk: RiskReview, Reason: fmt.Sprintf("dropping check constraint %s weakens data integrity", name)})
		}
		if newOK && (!oldOK || changed) {
			copy := newValue
			adds = append(adds, Operation{Kind: AddCheckConstraint, Table: table, AfterCheck: &copy, Risk: RiskReview, Reason: fmt.Sprintf("adding check constraint %s validates existing table data", name)})
		}
	}
	return drops, adds
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

func effectivePrimaryKey(table schema.Table) *schema.PrimaryKey {
	if table.PrimaryKey != nil {
		copy := *table.PrimaryKey
		copy.Columns = append([]string(nil), table.PrimaryKey.Columns...)
		return &copy
	}
	var columns []string
	for _, column := range table.Columns {
		if column.PrimaryKey {
			columns = append(columns, column.Name)
		}
	}
	if len(columns) == 0 {
		return nil
	}
	return &schema.PrimaryKey{Columns: columns}
}

func primaryKeysEqual(a, b *schema.PrimaryKey) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Name == b.Name && stringSlicesEqual(a.Columns, b.Columns)
}

func columnsEqual(a, b schema.Column) bool {
	return a == b
}

func indexesEqual(a, b schema.Index) bool {
	return a.Name == b.Name && a.Expression == b.Expression && a.Unique == b.Unique && a.Method == b.Method && a.Predicate == b.Predicate && stringSlicesEqual(a.Columns, b.Columns) && stringSlicesEqual(a.Include, b.Include)
}

func uniqueConstraintsEqual(a, b schema.UniqueConstraint) bool {
	return a.Name == b.Name && stringSlicesEqual(a.Columns, b.Columns)
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
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

func indexMap(table schema.Table) map[string]schema.Index {
	result := make(map[string]schema.Index, len(table.Indexes))
	for _, index := range table.Indexes {
		result[index.Name] = index
	}
	return result
}

func uniqueMap(table schema.Table) map[string]schema.UniqueConstraint {
	result := make(map[string]schema.UniqueConstraint, len(table.UniqueConstraints))
	for _, value := range table.UniqueConstraints {
		result[value.Name] = value
	}
	return result
}

func checkMap(table schema.Table) map[string]schema.CheckConstraint {
	result := make(map[string]schema.CheckConstraint, len(table.Checks))
	for _, value := range table.Checks {
		result[value.Name] = value
	}
	return result
}

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func unionKeys[T any](before, after map[string]T) []string {
	all := make(map[string]struct{}, len(before)+len(after))
	for key := range before {
		all[key] = struct{}{}
	}
	for key := range after {
		all[key] = struct{}{}
	}
	return sortedKeys(all)
}

func cloneTable(table schema.Table) schema.Table {
	result := table
	result.Columns = append([]schema.Column(nil), table.Columns...)
	if table.PrimaryKey != nil {
		primaryKey := *table.PrimaryKey
		primaryKey.Columns = append([]string(nil), table.PrimaryKey.Columns...)
		result.PrimaryKey = &primaryKey
	}
	result.UniqueConstraints = append([]schema.UniqueConstraint(nil), table.UniqueConstraints...)
	for i := range result.UniqueConstraints {
		result.UniqueConstraints[i].Columns = append([]string(nil), table.UniqueConstraints[i].Columns...)
	}
	result.Checks = append([]schema.CheckConstraint(nil), table.Checks...)
	result.Indexes = append([]schema.Index(nil), table.Indexes...)
	for i := range result.Indexes {
		result.Indexes[i] = cloneIndex(table.Indexes[i])
	}
	return result
}

func cloneIndex(index schema.Index) schema.Index {
	result := index
	result.Columns = append([]string(nil), index.Columns...)
	result.Include = append([]string(nil), index.Include...)
	return result
}

func cloneUnique(constraint schema.UniqueConstraint) schema.UniqueConstraint {
	result := constraint
	result.Columns = append([]string(nil), constraint.Columns...)
	return result
}
