// Package entity converts Go structs into Sevlumen ORM's schema model.
package entity

import (
	"fmt"
	"reflect"
	"strings"
	"time"
	"unicode"

	"github.com/sevlumen/orm/schema"
)

var tableNamerType = reflect.TypeOf((*TableNamer)(nil)).Elem()

// TableNamer lets an entity override the default snake_case table name.
type TableNamer interface {
	TableName() string
}

// Parse converts one or more Go structs into the schema intermediate representation.
func Parse(entities ...any) (schema.Schema, error) {
	if len(entities) == 0 {
		return schema.Schema{}, fmt.Errorf("entity: at least one entity is required")
	}

	result := schema.Schema{Tables: make([]schema.Table, 0, len(entities))}
	for _, value := range entities {
		table, err := parseEntity(value)
		if err != nil {
			return schema.Schema{}, err
		}
		result.Tables = append(result.Tables, table)
	}
	if err := result.Validate(); err != nil {
		return schema.Schema{}, err
	}
	return result, nil
}

func parseEntity(value any) (schema.Table, error) {
	if value == nil {
		return schema.Table{}, fmt.Errorf("entity: nil entity")
	}

	t := reflect.TypeOf(value)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return schema.Table{}, fmt.Errorf("entity: expected struct, got %s", t.Kind())
	}

	tableName := snakeCase(t.Name())
	if reflect.PointerTo(t).Implements(tableNamerType) {
		tableName = reflect.New(t).Interface().(TableNamer).TableName()
	} else if t.Implements(tableNamerType) {
		tableName = reflect.Zero(t).Interface().(TableNamer).TableName()
	}

	table := schema.Table{Name: tableName, Columns: make([]schema.Column, 0, t.NumField())}
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() || field.Tag.Get("orm") == "-" {
			continue
		}

		column, err := parseField(field)
		if err != nil {
			return schema.Table{}, fmt.Errorf("entity: %s.%s: %w", t.Name(), field.Name, err)
		}
		table.Columns = append(table.Columns, column)
	}
	return table, nil
}

func parseField(field reflect.StructField) (schema.Column, error) {
	options, err := parseTag(field.Tag.Get("orm"))
	if err != nil {
		return schema.Column{}, err
	}

	fieldType, nullable := dereference(field.Type)
	columnType := options["type"]
	if columnType == "" {
		columnType, err = inferPostgresType(fieldType)
		if err != nil {
			return schema.Column{}, err
		}
	}

	column := schema.Column{
		Name:       snakeCase(field.Name),
		Type:       columnType,
		Nullable:   nullable,
		PrimaryKey: options["primaryKey"] == "true",
		Unique:     options["unique"] == "true",
		Default:    options["default"],
	}
	if name := options["column"]; name != "" {
		column.Name = name
	}
	if options["nullable"] == "true" {
		column.Nullable = true
	}
	if options["notNull"] == "true" || column.PrimaryKey {
		column.Nullable = false
	}
	return column, nil
}

func parseTag(tag string) (map[string]string, error) {
	allowed := map[string]bool{
		"column": true, "type": true, "primaryKey": true, "unique": true,
		"notNull": true, "nullable": true, "default": true,
	}
	result := map[string]string{}
	for _, token := range strings.Split(tag, ";") {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		key, value, found := strings.Cut(token, ":")
		key = strings.TrimSpace(key)
		if !allowed[key] {
			return nil, fmt.Errorf("unknown orm tag option %q", key)
		}
		if _, exists := result[key]; exists {
			return nil, fmt.Errorf("duplicate orm tag option %q", key)
		}
		if !found {
			result[key] = "true"
			continue
		}
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, fmt.Errorf("orm tag option %q requires a value", key)
		}
		result[key] = value
	}
	if result["notNull"] == "true" && result["nullable"] == "true" {
		return nil, fmt.Errorf("notNull and nullable cannot be used together")
	}
	return result, nil
}

func dereference(t reflect.Type) (reflect.Type, bool) {
	nullable := false
	for t.Kind() == reflect.Pointer {
		nullable = true
		t = t.Elem()
	}
	return t, nullable
}

func inferPostgresType(t reflect.Type) (string, error) {
	if t == reflect.TypeOf(time.Time{}) {
		return "timestamptz", nil
	}

	switch t.Kind() {
	case reflect.String:
		return "text", nil
	case reflect.Bool:
		return "boolean", nil
	case reflect.Int8, reflect.Int16:
		return "smallint", nil
	case reflect.Int, reflect.Int32:
		return "integer", nil
	case reflect.Int64:
		return "bigint", nil
	case reflect.Float32:
		return "real", nil
	case reflect.Float64:
		return "double precision", nil
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return "bytea", nil
		}
	}
	return "", fmt.Errorf("unsupported Go type %s; use orm:\"type:...\"", t)
}

func snakeCase(value string) string {
	runes := []rune(value)
	var b strings.Builder
	for i, r := range runes {
		if unicode.IsUpper(r) && i > 0 {
			previous := runes[i-1]
			nextIsLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if unicode.IsLower(previous) || unicode.IsDigit(previous) || (unicode.IsUpper(previous) && nextIsLower) {
				b.WriteByte('_')
			}
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}
