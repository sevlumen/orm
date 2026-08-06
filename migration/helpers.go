package migration

import (
	"sort"

	"github.com/sevlumen/orm/schema"
)

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

func columnsEqual(a, b schema.Column) bool { return a == b }

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

func cloneEnum(enum schema.EnumType) schema.EnumType {
	result := enum
	result.Values = append([]string(nil), enum.Values...)
	return result
}
