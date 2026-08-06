package generator

import (
	"go/ast"
	"go/token"
)

const queryImportPath = "github.com/sevlumen/orm/postgres/query"

type packageModel struct {
	name       string
	entities   []entityModel
	imports    map[string]importModel
	queryAlias string
}

type importModel struct {
	alias    string
	path     string
	explicit bool
}

type entityModel struct {
	name      string
	tableName string
	fields    []fieldModel
	position  token.Position
}

type fieldModel struct {
	name       string
	columnName string
	typeText   string
	capability string
	position   token.Position
}

type sourceFile struct {
	astFile *ast.File
	imports map[string]importModel
}
