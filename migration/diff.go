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
	AddForeignKey        OperationKind = "add_foreign_key"
	DropForeignKey       OperationKind = "drop_foreign_key"
)

// Operation contains enough before/after state to render both up and down SQL.
type Operation struct {
	Kind             OperationKind
	Table            string
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
	case AddForeignKey:
		if operation.AfterForeignKey == nil {
			return fmt.Errorf("after foreign key is required")
		}
		return validateOperationForeignKey(operation.Table, *operation.AfterForeignKey)
	case DropForeignKey:
		if operation.BeforeForeignKey == nil {
			return fmt.Errorf("before foreign key is required")
		}
		return validateOperationForeignKey(operation.Table, *operation.BeforeForeignKey)
	default:
		return fmt.Errorf("unsupported kind %q", operation.Kind)
	}
}

func validateOperationTable(name string, table schema.Table) error {
	if table.Name != name {
		return fmt.Errorf("table name mismatch: operation=%q value=%q", name, table.Name)
	}
	table.ForeignKeys = nil
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

func validateOperationForeignKey(table string, foreignKey schema.ForeignKey) error {
	columnNames := append([]string(nil), foreignKey.Columns...)
	if table == foreignKey.ReferencedTable {
		columnNames = append(columnNames, foreignKey.ReferencedColumns...)
	}
	localColumns := syntheticColumns(columnNames)
	uniqueName := "__sevlumen_reference_key"
	if foreignKey.Name == uniqueName {
		uniqueName += "_2"
	}
	if table == foreignKey.ReferencedTable {
		value := schema.Table{
			Name:              table,
			Columns:           localColumns,
			UniqueConstraints: []schema.UniqueConstraint{{Name: uniqueName, Columns: append([]string(nil), foreignKey.ReferencedColumns...)}},
			ForeignKeys:       []schema.ForeignKey{foreignKey},
		}
		return (schema.Schema{Tables: []schema.Table{value}}).Validate()
	}
	referenced := schema.Table{
		Name:              foreignKey.ReferencedTable,
		Columns:           syntheticColumns(foreignKey.ReferencedColumns),
		UniqueConstraints: []schema.UniqueConstraint{{Name: uniqueName, Columns: append([]string(nil), foreignKey.ReferencedColumns...)}},
	}
	local := schema.Table{Name: table, Columns: localColumns, ForeignKeys: []schema.ForeignKey{foreignKey}}
	return (schema.Schema{Tables: []schema.Table{local, referenced}}).Validate()
}

func syntheticColumns(names []string) []schema.Column {
	seen := map[string]struct{}{}
	result := make([]schema.Column, 0, len(names))
	for _, name := range names {
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, schema.Column{Name: name, Type: "text", Nullable: true, Default: "'placeholder'"})
	}
	return result
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

	operations := make([]Operation, 0, len(dropForeignKeys)+len(dropDependencies)+len(dropTables)+len(createTables)+len(addColumns)+len(alterColumns)+len(dropColumns)+len(addDependencies)+len(addForeignKeys))
	operations = append(operations, dropForeignKeys...)
	operations = append(operations, dropDependencies...)
	operations = append(operations, dropTables...)
	operations = append(operations, createTables...)
	operations = append(operations, addColumns...)
	operations = append(operations, alterColumns...)
	operations = append(operations, dropColumns...)
	operations = append(operations, addDependencies...)
	operations = append(operations, addForeignKeys...)
	plan := Plan{Operations: operations}
	if err := plan.Validate(); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func diffForeignKeys(before, after schema.Schema) ([]Operation, []Operation) {
	beforeMap, afterMap := foreignKeyMap(before), foreignKeyMap(after)
	beforeTables, afterTables := tableMap(before), tableMap(after)
	var drops, adds []Operation
	for _, key := range unionKeys(beforeMap, afterMap) {
		old, oldOK := beforeMap[key]
		newValue, newOK := afterMap[key]
		changed := oldOK && newOK && !foreignKeysEqual(old.ForeignKey, newValue.ForeignKey)
		recreate := oldOK && newOK && !changed && foreignKeyNeedsRecreate(old, beforeTables, afterTables)
		if oldOK && (!newOK || changed || recreate) {
			copy := cloneForeignKey(old.ForeignKey)
			drops = append(drops, Operation{
				Kind:             DropForeignKey,
				Table:            old.Table,
				BeforeForeignKey: &copy,
				Risk:             RiskReview,
				Reason:           fmt.Sprintf("dropping foreign key %s temporarily weakens referential integrity", old.ForeignKey.Name),
			})
		}
		if newOK && (!oldOK || changed || recreate) {
			copy := cloneForeignKey(newValue.ForeignKey)
			adds = append(adds, Operation{
				Kind:            AddForeignKey,
				Table:           newValue.Table,
				AfterForeignKey: &copy,
				Risk:            RiskReview,
				Reason:          fmt.Sprintf("adding foreign key %s validates existing data and can lock both tables", newValue.ForeignKey.Name),
			})
		}
	}
	return drops, adds
}

type foreignKeyEntry struct {
	Table      string
	ForeignKey schema.ForeignKey
}

func foreignKeyMap(model schema.Schema) map[string]foreignKeyEntry {
	result := map[string]foreignKeyEntry{}
	for _, table := range model.Tables {
		for _, foreignKey := range table.ForeignKeys {
			key := table.Name + "\x00" + foreignKey.Name
			result[key] = foreignKeyEntry{Table: table.Name, ForeignKey: foreignKey}
		}
	}
	return result
}

func foreignKeyNeedsRecreate(entry foreignKeyEntry, beforeTables, afterTables map[string]schema.Table) bool {
	beforeLocal, localBeforeOK := beforeTables[entry.Table]
	afterLocal, localAfterOK := afterTables[entry.Table]
	beforeReferenced, referencedBeforeOK := beforeTables[entry.ForeignKey.ReferencedTable]
	afterReferenced, referencedAfterOK := afterTables[entry.ForeignKey.ReferencedTable]
	if !localBeforeOK || !localAfterOK || !referencedBeforeOK || !referencedAfterOK {
		return true
	}
	for _, column := range entry.ForeignKey.Columns {
		if columnTypeChanged(beforeLocal, afterLocal, column) {
			return true
		}
	}
	for _, column := range entry.ForeignKey.ReferencedColumns {
		if columnTypeChanged(beforeReferenced, afterReferenced, column) {
			return true
		}
	}
	return referencedKeysChanged(beforeReferenced, afterReferenced)
}

func columnTypeChanged(before, after schema.Table, name string) bool {
	beforeColumn, beforeOK := columnMap(before)[name]
	afterColumn, afterOK := columnMap(after)[name]
	return !beforeOK || !afterOK || beforeColumn.Type != afterColumn.Type
}

func referencedKeysChanged(before, after schema.Table) bool {
	if !primaryKeysEqual(effectivePrimaryKey(before), effectivePrimaryKey(after)) {
		return true
	}
	if !uniqueMapsEqual(uniqueMap(before), uniqueMap(after)) {
		return true
	}
	beforeColumns, afterColumns := columnMap(before), columnMap(after)
	for _, name := range unionKeys(beforeColumns, afterColumns) {
		beforeColumn, beforeOK := beforeColumns[name]
		afterColumn, afterOK := afterColumns[name]
		if beforeOK != afterOK || beforeOK && beforeColumn.Unique != afterColumn.Unique {
			return true
		}
	}
	return !uniqueIndexMapsEqual(uniqueIndexMap(before), uniqueIndexMap(after))
}

func uniqueMapsEqual(before, after map[string]schema.UniqueConstraint) bool {
	if len(before) != len(after) {
		return false
	}
	for name, value := range before {
		other, exists := after[name]
		if !exists || !uniqueConstraintsEqual(value, other) {
			return false
		}
	}
	return true
}

func uniqueIndexMapsEqual(before, after map[string]schema.Index) bool {
	if len(before) != len(after) {
		return false
	}
	for name, value := range before {
		other, exists := after[name]
		if !exists || !indexesEqual(value, other) {
			return false
		}
	}
	return true
}

func uniqueIndexMap(table schema.Table) map[string]schema.Index {
	result := map[string]schema.Index{}
	for _, index := range table.Indexes {
		if index.Unique && index.Expression == "" && strings.TrimSpace(index.Predicate) == "" {
			result[index.Name] = index
		}
	}
	return result
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

func foreignKeysEqual(a, b schema.ForeignKey) bool {
	return a.Name == b.Name && a.ReferencedTable == b.ReferencedTable && a.OnDelete == b.OnDelete && a.OnUpdate == b.OnUpdate && a.Deferrable == b.Deferrable && a.InitiallyDeferred == b.InitiallyDeferred && stringSlicesEqual(a.Columns, b.Columns) && stringSlicesEqual(a.ReferencedColumns, b.ReferencedColumns)
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
	result.ForeignKeys = append([]schema.ForeignKey(nil), table.ForeignKeys...)
	for i := range result.ForeignKeys {
		result.ForeignKeys[i] = cloneForeignKey(table.ForeignKeys[i])
	}
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

func cloneForeignKey(foreignKey schema.ForeignKey) schema.ForeignKey {
	result := foreignKey
	result.Columns = append([]string(nil), foreignKey.Columns...)
	result.ReferencedColumns = append([]string(nil), foreignKey.ReferencedColumns...)
	return result
}
