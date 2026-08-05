package entity

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/sevlumen/orm/schema"
)

// TableNamer lets an entity override the default snake_case table name.
type TableNamer interface {
	TableName() string
}

// Parse converts one or more Go structs into the schema intermediate representation.
func Parse(entities ...any) (schema.Schema, error) {
	result := schema.Schema{}
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
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return schema.Table{}, fmt.Errorf("entity: expected struct, got %s", t.Kind())
	}

	tableName := snakeCase(t.Name())
	if namer, ok := value.(TableNamer); ok {
		tableName = namer.TableName()
	} else if reflect.PointerTo(t).Implements(reflect.TypeOf((*TableNamer)(nil)).Elem()) {
		instance := reflect.New(t).Interface().(TableNamer)
		tableName = instance.TableName()
	}

	table := schema.Table{Name: tableName}
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
	options := parseTag(field.Tag.Get("orm"))
	columnType, nullable, err := postgresType(field.Type)
	if err != nil {
		return schema.Column{}, err
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
	if typ := options["type"]; typ != "" {
		column.Type = typ
	}
	if options["notNull"] == "true" || column.PrimaryKey {
		column.Nullable = false
	}
	return column, nil
}

func parseTag(tag string) map[string]string {
	result := map[string]string{}
	for _, token := range strings.Split(tag, ";") {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		key, value, found := strings.Cut(token, ":")
		if !found {
			result[key] = "true"
			continue
		}
		result[key] = value
	}
	return result
}

func postgresType(t reflect.Type) (string, bool, error) {
	nullable := false
	if t.Kind() == reflect.Pointer {
		nullable = true
		t = t.Elem()
	}
	if t == reflect.TypeOf(time.Time{}) {
		return "timestamptz", nullable, nil
	}

	switch t.Kind() {
	case reflect.String:
		return "text", nullable, nil
	case reflect.Bool:
		return "boolean", nullable, nil
	case reflect.Int, reflect.Int32:
		return "integer", nullable, nil
	case reflect.Int64:
		return "bigint", nullable, nil
	case reflect.Float32:
		return "real", nullable, nil
	case reflect.Float64:
		return "double precision", nullable, nil
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return "bytea", nullable, nil
		}
	}
	return "", false, fmt.Errorf("unsupported Go type %s; use orm:\"type:...\"", t)
}

func snakeCase(value string) string {
	var b strings.Builder
	for i, r := range value {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('_')
		}
		b.WriteRune(r)
	}
	return strings.ToLower(b.String())
}
