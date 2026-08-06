package entity

import (
	"fmt"

	"github.com/sevlumen/orm/schema"
)

// SchemaBuilder configures PostgreSQL-native objects around parsed entities.
type SchemaBuilder struct {
	model   *schema.Schema
	problem error
}

// ParseWithSchema parses entities and then applies schema-level configuration.
func ParseWithSchema(configure func(*SchemaBuilder), entities ...any) (result schema.Schema, err error) {
	result, err = Parse(entities...)
	if err != nil {
		return schema.Schema{}, err
	}
	if configure == nil {
		return result, nil
	}

	builder := &SchemaBuilder{model: &result}
	defer func() {
		if recovered := recover(); recovered != nil {
			result = schema.Schema{}
			err = fmt.Errorf("entity: schema configuration panicked: %v", recovered)
		}
	}()
	configure(builder)
	if builder.problem != nil {
		return schema.Schema{}, builder.problem
	}
	if err := result.Validate(); err != nil {
		return schema.Schema{}, err
	}
	return result, nil
}

// Extension declares a required PostgreSQL extension.
func (b *SchemaBuilder) Extension(name string) {
	if b == nil || b.problem != nil {
		return
	}
	b.model.Extensions = append(b.model.Extensions, schema.Extension{Name: name})
}

// Enum declares an ordered PostgreSQL enum type.
func (b *SchemaBuilder) Enum(name string, values ...string) {
	if b == nil || b.problem != nil {
		return
	}
	b.model.Enums = append(b.model.Enums, schema.EnumType{Name: name, Values: append([]string(nil), values...)})
}
