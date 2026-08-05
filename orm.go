package orm

import (
	"github.com/sevlumen/orm/entity"
	"github.com/sevlumen/orm/postgres"
)

// PostgreSQLSchema converts Go entities into PostgreSQL CREATE TABLE SQL.
func PostgreSQLSchema(entities ...any) (string, error) {
	model, err := entity.Parse(entities...)
	if err != nil {
		return "", err
	}
	return postgres.RenderCreateSchema(model)
}
