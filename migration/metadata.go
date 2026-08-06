package migration

import (
	"fmt"
	"strings"

	"github.com/sevlumen/orm/schema"
)

func validateMetadataOperation(operation Operation) error {
	switch operation.Kind {
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
		return fmt.Errorf("unsupported metadata kind %q", operation.Kind)
	}
}

func validateOperationIndex(table string, index schema.Index) error {
	columnNames := append([]string(nil), index.Columns...)
	columnNames = append(columnNames, index.Include...)
	if len(columnNames) == 0 {
		columnNames = []string{"placeholder"}
	}
	columns := syntheticColumns(columnNames)
	return (schema.Schema{Tables: []schema.Table{{Name: table, Columns: columns, Indexes: []schema.Index{index}}}}).Validate()
}

func validateOperationUnique(table string, constraint schema.UniqueConstraint) error {
	columns := syntheticColumns(constraint.Columns)
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
	uniqueName := internalName("reference_key", table, foreignKey.Name)
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

func internalName(prefix, table, value string) string {
	candidate := "sl_" + prefix + "_" + table + "_" + value
	if len(candidate) <= 63 {
		return candidate
	}
	return candidate[:63]
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
