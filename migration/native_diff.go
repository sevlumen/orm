package migration

import (
	"fmt"

	"github.com/sevlumen/orm/schema"
)

// diffNativeObjects returns operations that must run before table operations and
// operations that must run after table operations.
func diffNativeObjects(before, after schema.Schema) ([]Operation, []Operation, error) {
	beforeExtensions, afterExtensions := extensionMap(before), extensionMap(after)
	beforeEnums, afterEnums := enumMap(before), enumMap(after)
	var beforeTables, afterTables []Operation

	for _, name := range unionKeys(beforeExtensions, afterExtensions) {
		old, oldOK := beforeExtensions[name]
		newValue, newOK := afterExtensions[name]
		switch {
		case oldOK && !newOK:
			return nil, nil, fmt.Errorf("migration: removing extension %q requires an explicit migration", old.Name)
		case !oldOK && newOK:
			copy := newValue
			beforeTables = append(beforeTables, Operation{
				Kind:           CreateExtension,
				AfterExtension: &copy,
				Risk:           RiskReview,
				Reason:         fmt.Sprintf("extension %s is retained during generated rollback because it may predate this migration", name),
			})
		}
	}

	for _, name := range unionKeys(beforeEnums, afterEnums) {
		old, oldOK := beforeEnums[name]
		newValue, newOK := afterEnums[name]
		switch {
		case oldOK && newOK && !enumTypesEqual(old, newValue):
			return nil, nil, fmt.Errorf("migration: changing enum %q values requires an explicit migration", name)
		case !oldOK && newOK:
			copy := cloneEnum(newValue)
			beforeTables = append(beforeTables, Operation{Kind: CreateEnum, AfterEnum: &copy})
		case oldOK && !newOK:
			copy := cloneEnum(old)
			afterTables = append(afterTables, Operation{
				Kind:       DropEnum,
				BeforeEnum: &copy,
				Risk:       RiskDestructive,
				Reason:     fmt.Sprintf("dropping enum %s removes a database type and requires all dependent columns to be removed first", name),
			})
		}
	}
	return beforeTables, afterTables, nil
}

func extensionMap(model schema.Schema) map[string]schema.Extension {
	result := make(map[string]schema.Extension, len(model.Extensions))
	for _, value := range model.Extensions {
		result[value.Name] = value
	}
	return result
}

func enumMap(model schema.Schema) map[string]schema.EnumType {
	result := make(map[string]schema.EnumType, len(model.Enums))
	for _, value := range model.Enums {
		result[value.Name] = value
	}
	return result
}

func enumTypesEqual(a, b schema.EnumType) bool {
	return a.Name == b.Name && stringSlicesEqual(a.Values, b.Values)
}
