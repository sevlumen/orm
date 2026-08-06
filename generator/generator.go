// Package generator creates typed query metadata and direct row scanners from Go structs.
package generator

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

const queryImportPath = "github.com/sevlumen/orm/postgres/query"

// Config controls one generation run.
type Config struct {
	Dir    string
	Types  []string
	Output string
	Check  bool
}

// StaleError reports that check mode found outdated generated code.
type StaleError struct {
	Path string
}

func (e *StaleError) Error() string {
	return fmt.Sprintf("ormgen: generated file %s is stale", e.Path)
}

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

// Generate parses the selected package and returns gofmt-formatted generated code.
func Generate(config Config) ([]byte, error) {
	model, err := load(config)
	if err != nil {
		return nil, err
	}
	data, err := render(model)
	if err != nil {
		return nil, err
	}
	formatted, err := format.Source(data)
	if err != nil {
		return nil, fmt.Errorf("ormgen: format generated source: %w", err)
	}
	return formatted, nil
}

// Write generates code and atomically writes it, or verifies it in check mode.
func Write(config Config) error {
	data, err := Generate(config)
	if err != nil {
		return err
	}
	dir, output, err := resolvePaths(config)
	if err != nil {
		return err
	}
	_ = dir
	current, readErr := os.ReadFile(output)
	if config.Check {
		if readErr != nil {
			if errors.Is(readErr, os.ErrNotExist) {
				return &StaleError{Path: output}
			}
			return fmt.Errorf("ormgen: read generated file: %w", readErr)
		}
		if !bytes.Equal(current, data) {
			return &StaleError{Path: output}
		}
		return nil
	}
	if readErr == nil && bytes.Equal(current, data) {
		return nil
	}
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return fmt.Errorf("ormgen: read generated file: %w", readErr)
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return fmt.Errorf("ormgen: create output directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(output), ".ormgen-*.tmp")
	if err != nil {
		return fmt.Errorf("ormgen: create temporary output: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("ormgen: write temporary output: %w", err)
	}
	if err := temporary.Chmod(0o644); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("ormgen: set output permissions: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("ormgen: sync temporary output: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("ormgen: close temporary output: %w", err)
	}
	if err := os.Rename(temporaryName, output); err != nil {
		return fmt.Errorf("ormgen: replace generated file: %w", err)
	}
	return nil
}

func load(config Config) (packageModel, error) {
	dir, output, err := resolvePaths(config)
	if err != nil {
		return packageModel{}, err
	}
	typeNames, err := normalizeTypes(config.Types)
	if err != nil {
		return packageModel{}, err
	}
	files, packageName, fileSet, err := parsePackage(dir, output)
	if err != nil {
		return packageModel{}, err
	}

	declarations := map[string]token.Position{}
	structs := map[string]struct {
		typeSpec *ast.TypeSpec
		genDecl  *ast.GenDecl
		file     sourceFile
	}{}
	tableMethods := map[string]*ast.FuncDecl{}
	for _, file := range files {
		for _, declaration := range file.astFile.Decls {
			switch value := declaration.(type) {
			case *ast.GenDecl:
				for _, spec := range value.Specs {
					typeSpec, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					declarations[typeSpec.Name.Name] = fileSet.Position(typeSpec.Pos())
					if _, ok := typeSpec.Type.(*ast.StructType); ok {
						structs[typeSpec.Name.Name] = struct {
							typeSpec *ast.TypeSpec
							genDecl  *ast.GenDecl
							file     sourceFile
						}{typeSpec: typeSpec, genDecl: value, file: file}
					}
				}
			case *ast.FuncDecl:
				declarations[value.Name.Name] = fileSet.Position(value.Pos())
				if value.Name.Name == "TableName" {
					if receiver := receiverTypeName(value); receiver != "" {
						tableMethods[receiver] = value
					}
				}
			}
		}
	}

	model := packageModel{name: packageName, imports: map[string]importModel{}}
	model.queryAlias = uniqueAlias("ormquery", files)
	for _, typeName := range typeNames {
		entry, exists := structs[typeName]
		if !exists {
			position, declared := declarations[typeName]
			if declared {
				return packageModel{}, diagnostic(position, "%s is not a struct type", typeName)
			}
			return packageModel{}, fmt.Errorf("ormgen: type %s was not found in package %s", typeName, packageName)
		}
		if entry.typeSpec.TypeParams != nil && len(entry.typeSpec.TypeParams.List) > 0 {
			return packageModel{}, diagnostic(fileSet.Position(entry.typeSpec.Pos()), "generic entity %s is not supported", typeName)
		}
		entity, imports, err := parseEntity(fileSet, entry.typeSpec, entry.genDecl, entry.file, tableMethods[typeName])
		if err != nil {
			return packageModel{}, err
		}
		for alias, imported := range imports {
			if alias == model.queryAlias && imported.path != queryImportPath {
				return packageModel{}, diagnostic(entity.position, "import alias %q conflicts with generated query import", alias)
			}
			if previous, exists := model.imports[alias]; exists && previous.path != imported.path {
				return packageModel{}, diagnostic(entity.position, "import alias %q resolves to both %q and %q", alias, previous.path, imported.path)
			}
			model.imports[alias] = imported
		}
		generatedNames := []string{entity.name + "ORM", entity.name + "ORMMetadata", "new" + entity.name + "ORMMetadata", "scan" + entity.name + "ORM"}
		for _, generatedName := range generatedNames {
			if position, exists := declarations[generatedName]; exists {
				return packageModel{}, diagnostic(position, "generated declaration %s conflicts with an existing declaration", generatedName)
			}
		}
		model.entities = append(model.entities, entity)
	}
	sort.Slice(model.entities, func(i, j int) bool { return model.entities[i].name < model.entities[j].name })
	return model, nil
}

func resolvePaths(config Config) (string, string, error) {
	dir := strings.TrimSpace(config.Dir)
	if dir == "" {
		dir = "."
	}
	absoluteDir, err := filepath.Abs(dir)
	if err != nil {
		return "", "", fmt.Errorf("ormgen: resolve package directory: %w", err)
	}
	info, err := os.Stat(absoluteDir)
	if err != nil {
		return "", "", fmt.Errorf("ormgen: stat package directory: %w", err)
	}
	if !info.IsDir() {
		return "", "", fmt.Errorf("ormgen: package path %s is not a directory", absoluteDir)
	}
	output := strings.TrimSpace(config.Output)
	if output == "" {
		output = "orm_gen.go"
	}
	if !filepath.IsAbs(output) {
		output = filepath.Join(absoluteDir, output)
	}
	output = filepath.Clean(output)
	relative, err := filepath.Rel(absoluteDir, output)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("ormgen: output must be inside package directory")
	}
	if filepath.Ext(output) != ".go" {
		return "", "", fmt.Errorf("ormgen: output must have .go extension")
	}
	return absoluteDir, output, nil
}

func normalizeTypes(values []string) ([]string, error) {
	seen := map[string]struct{}{}
	var result []string
	for _, value := range values {
		for _, item := range strings.Split(value, ",") {
			name := strings.TrimSpace(item)
			if name == "" {
				continue
			}
			if !token.IsIdentifier(name) || !ast.IsExported(name) {
				return nil, fmt.Errorf("ormgen: entity type %q must be an exported Go identifier", name)
			}
			if _, exists := seen[name]; exists {
				return nil, fmt.Errorf("ormgen: entity type %q is configured more than once", name)
			}
			seen[name] = struct{}{}
			result = append(result, name)
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("ormgen: at least one entity type is required")
	}
	sort.Strings(result)
	return result, nil
}

func parsePackage(dir, output string) ([]sourceFile, string, *token.FileSet, error) {
	fileSet := token.NewFileSet()
	packages, err := parser.ParseDir(fileSet, dir, func(info os.FileInfo) bool {
		name := info.Name()
		return strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") && filepath.Join(dir, name) != output
	}, parser.ParseComments)
	if err != nil {
		return nil, "", nil, fmt.Errorf("ormgen: parse package: %w", err)
	}
	if len(packages) != 1 {
		return nil, "", nil, fmt.Errorf("ormgen: expected one package in %s, found %d", dir, len(packages))
	}
	var packageName string
	var parsed *ast.Package
	for name, value := range packages {
		packageName, parsed = name, value
	}
	filenames := make([]string, 0, len(parsed.Files))
	for filename := range parsed.Files {
		filenames = append(filenames, filename)
	}
	sort.Strings(filenames)
	files := make([]sourceFile, 0, len(filenames))
	for _, filename := range filenames {
		file := parsed.Files[filename]
		imports, err := importsForFile(file)
		if err != nil {
			return nil, "", nil, diagnostic(fileSet.Position(file.Pos()), "%v", err)
		}
		files = append(files, sourceFile{astFile: file, imports: imports})
	}
	return files, packageName, fileSet, nil
}

func importsForFile(file *ast.File) (map[string]importModel, error) {
	result := map[string]importModel{}
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			return nil, fmt.Errorf("invalid import path %s", spec.Path.Value)
		}
		alias := ""
		explicit := false
		if spec.Name != nil {
			alias = spec.Name.Name
			explicit = true
			if alias == "." || alias == "_" {
				continue
			}
		} else {
			alias = filepath.Base(path)
		}
		result[alias] = importModel{alias: alias, path: path, explicit: explicit}
	}
	return result, nil
}

func parseEntity(fileSet *token.FileSet, typeSpec *ast.TypeSpec, genDecl *ast.GenDecl, file sourceFile, tableMethod *ast.FuncDecl) (entityModel, map[string]importModel, error) {
	structure := typeSpec.Type.(*ast.StructType)
	entity := entityModel{name: typeSpec.Name.Name, tableName: snakeCase(typeSpec.Name.Name), position: fileSet.Position(typeSpec.Pos())}
	directive, directiveFound, err := tableDirective(typeSpec.Doc, genDecl.Doc)
	if err != nil {
		return entityModel{}, nil, diagnostic(entity.position, "%v", err)
	}
	if directiveFound {
		entity.tableName = directive
	} else if tableMethod != nil {
		literal, literalErr := tableNameLiteral(tableMethod)
		if literalErr != nil {
			return entityModel{}, nil, diagnostic(fileSet.Position(tableMethod.Pos()), "%v", literalErr)
		}
		entity.tableName = literal
	}
	if err := validateDatabaseIdentifier("table", entity.tableName); err != nil {
		return entityModel{}, nil, diagnostic(entity.position, "%v", err)
	}

	usedImports := map[string]importModel{}
	seenColumns := map[string]token.Position{}
	for _, field := range structure.Fields.List {
		position := fileSet.Position(field.Pos())
		if len(field.Names) == 0 {
			return entityModel{}, nil, diagnostic(position, "embedded field in %s is not supported", entity.name)
		}
		if len(field.Names) != 1 {
			return entityModel{}, nil, diagnostic(position, "field declaration in %s must declare exactly one name", entity.name)
		}
		name := field.Names[0].Name
		tag, err := ormTag(field.Tag)
		if err != nil {
			return entityModel{}, nil, diagnostic(position, "%v", err)
		}
		if !ast.IsExported(name) {
			if tag != "" {
				return entityModel{}, nil, diagnostic(position, "unexported field %s.%s cannot have an orm tag", entity.name, name)
			}
			continue
		}
		if tag == "-" {
			continue
		}
		options, err := parseOptions(tag)
		if err != nil {
			return entityModel{}, nil, diagnostic(position, "%s.%s: %v", entity.name, name, err)
		}
		columnName := snakeCase(name)
		if configured := options["column"]; configured != "" {
			columnName = configured
		}
		if err := validateDatabaseIdentifier("column", columnName); err != nil {
			return entityModel{}, nil, diagnostic(position, "%v", err)
		}
		if previous, exists := seenColumns[columnName]; exists {
			return entityModel{}, nil, diagnostic(position, "column %q duplicates field at %s", columnName, previous)
		}
		seenColumns[columnName] = position

		typeText, err := formatNode(fileSet, field.Type)
		if err != nil {
			return entityModel{}, nil, diagnostic(position, "format field type: %v", err)
		}
		aliases := selectorAliases(field.Type)
		for alias := range aliases {
			imported, exists := file.imports[alias]
			if !exists {
				return entityModel{}, nil, diagnostic(position, "cannot resolve package alias %q in field type; use an explicit import alias", alias)
			}
			usedImports[alias] = imported
		}
		capability, err := mutationCapability(options)
		if err != nil {
			return entityModel{}, nil, diagnostic(position, "%s.%s: %v", entity.name, name, err)
		}
		entity.fields = append(entity.fields, fieldModel{name: name, columnName: columnName, typeText: typeText, capability: capability, position: position})
	}
	if len(entity.fields) == 0 {
		return entityModel{}, nil, diagnostic(entity.position, "entity %s has no selectable exported fields", entity.name)
	}
	return entity, usedImports, nil
}

func tableDirective(groups ...*ast.CommentGroup) (string, bool, error) {
	var value string
	found := false
	for _, group := range groups {
		if group == nil {
			continue
		}
		for _, comment := range group.List {
			text := strings.TrimSpace(strings.TrimPrefix(comment.Text, "//"))
			if strings.HasPrefix(text, "orm:table") {
				name := strings.TrimSpace(strings.TrimPrefix(text, "orm:table"))
				if name == "" {
					return "", false, fmt.Errorf("orm:table directive requires a table name")
				}
				if found && value != name {
					return "", false, fmt.Errorf("orm:table directive is configured more than once")
				}
				value, found = name, true
			}
		}
	}
	return value, found, nil
}

func tableNameLiteral(method *ast.FuncDecl) (string, error) {
	if method.Type.Params != nil && len(method.Type.Params.List) != 0 {
		return "", fmt.Errorf("TableName method must not accept parameters")
	}
	if method.Body == nil || len(method.Body.List) != 1 {
		return "", fmt.Errorf("TableName method must return one string literal or use //orm:table")
	}
	statement, ok := method.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(statement.Results) != 1 {
		return "", fmt.Errorf("TableName method must return one string literal or use //orm:table")
	}
	literal, ok := statement.Results[0].(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "", fmt.Errorf("TableName method must return one string literal or use //orm:table")
	}
	value, err := strconv.Unquote(literal.Value)
	if err != nil || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("TableName method has an invalid string literal")
	}
	return value, nil
}

func receiverTypeName(method *ast.FuncDecl) string {
	if method.Recv == nil || len(method.Recv.List) != 1 {
		return ""
	}
	typeExpression := method.Recv.List[0].Type
	if pointer, ok := typeExpression.(*ast.StarExpr); ok {
		typeExpression = pointer.X
	}
	identifier, _ := typeExpression.(*ast.Ident)
	if identifier == nil {
		return ""
	}
	return identifier.Name
}

func ormTag(tag *ast.BasicLit) (string, error) {
	if tag == nil {
		return "", nil
	}
	value, err := strconv.Unquote(tag.Value)
	if err != nil {
		return "", fmt.Errorf("invalid struct tag")
	}
	return reflectStructTag(value).get("orm")
}

type reflectStructTag string

func (tag reflectStructTag) get(key string) (string, error) {
	value := string(tag)
	for value != "" {
		value = strings.TrimLeft(value, " ")
		if value == "" {
			break
		}
		index := strings.IndexByte(value, ':')
		if index <= 0 || index+1 >= len(value) || value[index+1] != '"' {
			return "", fmt.Errorf("malformed struct tag")
		}
		name := value[:index]
		value = value[index+1:]
		quotedEnd := 1
		for quotedEnd < len(value) {
			if value[quotedEnd] == '\\' {
				quotedEnd += 2
				continue
			}
			if value[quotedEnd] == '"' {
				break
			}
			quotedEnd++
		}
		if quotedEnd >= len(value) {
			return "", fmt.Errorf("malformed struct tag")
		}
		quoted := value[:quotedEnd+1]
		decoded, err := strconv.Unquote(quoted)
		if err != nil {
			return "", fmt.Errorf("malformed struct tag")
		}
		value = value[quotedEnd+1:]
		if name == key {
			return decoded, nil
		}
	}
	return "", nil
}

func parseOptions(tag string) (map[string]string, error) {
	allowed := map[string]bool{
		"column": true, "type": true, "primaryKey": true, "unique": true,
		"notNull": true, "nullable": true, "default": true, "generated": true,
		"readOnly": true, "insertOnly": true, "updateOnly": true,
	}
	result := map[string]string{}
	for _, tokenValue := range strings.Split(tag, ";") {
		tokenValue = strings.TrimSpace(tokenValue)
		if tokenValue == "" {
			continue
		}
		key, value, found := strings.Cut(tokenValue, ":")
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
	return result, nil
}

func mutationCapability(options map[string]string) (string, error) {
	configured := []string{}
	for _, name := range []string{"readOnly", "insertOnly", "updateOnly"} {
		if options[name] == "true" {
			configured = append(configured, name)
		}
	}
	if len(configured) > 1 {
		return "", fmt.Errorf("mutation capability tags %s cannot be combined", strings.Join(configured, ", "))
	}
	if options["generated"] != "" {
		if len(configured) > 0 && configured[0] != "readOnly" {
			return "", fmt.Errorf("generated columns must be read-only")
		}
		return "readOnly", nil
	}
	if len(configured) == 1 {
		return configured[0], nil
	}
	return "mutable", nil
}

func selectorAliases(expression ast.Expr) map[string]struct{} {
	result := map[string]struct{}{}
	ast.Inspect(expression, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		identifier, ok := selector.X.(*ast.Ident)
		if ok {
			result[identifier.Name] = struct{}{}
		}
		return true
	})
	return result
}

func uniqueAlias(base string, files []sourceFile) string {
	used := map[string]struct{}{}
	for _, file := range files {
		for alias := range file.imports {
			used[alias] = struct{}{}
		}
	}
	alias := base
	for suffix := 2; ; suffix++ {
		if _, exists := used[alias]; !exists {
			return alias
		}
		alias = fmt.Sprintf("%s%d", base, suffix)
	}
}

func render(model packageModel) ([]byte, error) {
	var output bytes.Buffer
	output.WriteString("// Code generated by ormgen. DO NOT EDIT.\n\n")
	fmt.Fprintf(&output, "package %s\n\n", model.name)

	imports := make([]importModel, 0, len(model.imports)+1)
	imports = append(imports, importModel{alias: model.queryAlias, path: queryImportPath, explicit: true})
	for _, imported := range model.imports {
		imports = append(imports, imported)
	}
	sort.Slice(imports, func(i, j int) bool {
		if imports[i].path == imports[j].path {
			return imports[i].alias < imports[j].alias
		}
		return imports[i].path < imports[j].path
	})
	output.WriteString("import (\n")
	for _, imported := range imports {
		if imported.explicit || imported.alias != filepath.Base(imported.path) {
			fmt.Fprintf(&output, "\t%s %q\n", imported.alias, imported.path)
		} else {
			fmt.Fprintf(&output, "\t%q\n", imported.path)
		}
	}
	output.WriteString(")\n\n")

	for _, entity := range model.entities {
		fmt.Fprintf(&output, "type %sORMMetadata struct {\n", entity.name)
		fmt.Fprintf(&output, "\tTable *%s.Table[%s]\n", model.queryAlias, entity.name)
		for _, field := range entity.fields {
			fmt.Fprintf(&output, "\t%s %s.Column[%s, %s]\n", field.name, model.queryAlias, entity.name, field.typeText)
		}
		output.WriteString("}\n\n")
		fmt.Fprintf(&output, "var %sORM = new%sORMMetadata()\n\n", entity.name, entity.name)
		fmt.Fprintf(&output, "func new%sORMMetadata() %sORMMetadata {\n", entity.name, entity.name)
		fmt.Fprintf(&output, "\ttable, err := %s.NewTable[%s](%q, []string{", model.queryAlias, entity.name, entity.tableName)
		for index, field := range entity.fields {
			if index > 0 {
				output.WriteString(", ")
			}
			fmt.Fprintf(&output, "%q", field.columnName)
		}
		fmt.Fprintf(&output, "}, scan%sORM)\n", entity.name)
		fmt.Fprintf(&output, "\tif err != nil { panic(%q + err.Error()) }\n", "ormgen: generated "+entity.name+" table metadata: ")
		for _, field := range entity.fields {
			option := ""
			switch field.capability {
			case "readOnly":
				option = ", " + model.queryAlias + ".ReadOnlyColumn()"
			case "insertOnly":
				option = ", " + model.queryAlias + ".InsertOnlyColumn()"
			case "updateOnly":
				option = ", " + model.queryAlias + ".UpdateOnlyColumn()"
			}
			fmt.Fprintf(&output, "\t%s, err := %s.NewColumn[%s, %s](table, %q%s)\n", lowerFirst(field.name), model.queryAlias, entity.name, field.typeText, field.columnName, option)
			fmt.Fprintf(&output, "\tif err != nil { panic(%q + err.Error()) }\n", "ormgen: generated "+entity.name+"."+field.name+" metadata: ")
		}
		fmt.Fprintf(&output, "\treturn %sORMMetadata{Table: table", entity.name)
		for _, field := range entity.fields {
			fmt.Fprintf(&output, ", %s: %s", field.name, lowerFirst(field.name))
		}
		output.WriteString("}\n}\n\n")
		fmt.Fprintf(&output, "func scan%sORM(row %s.RowScanner) (%s, error) {\n", entity.name, model.queryAlias, entity.name)
		fmt.Fprintf(&output, "\tvar value %s\n", entity.name)
		output.WriteString("\terr := row.Scan(")
		for index, field := range entity.fields {
			if index > 0 {
				output.WriteString(", ")
			}
			fmt.Fprintf(&output, "&value.%s", field.name)
		}
		output.WriteString(")\n\treturn value, err\n}\n\n")
	}
	return output.Bytes(), nil
}

func formatNode(fileSet *token.FileSet, node ast.Node) (string, error) {
	var buffer bytes.Buffer
	if err := format.Node(&buffer, fileSet, node); err != nil {
		return "", err
	}
	return buffer.String(), nil
}

func validateDatabaseIdentifier(kind, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s name is required", kind)
	}
	if len(value) > 63 {
		return fmt.Errorf("%s name %q exceeds 63 bytes", kind, value)
	}
	if strings.ContainsRune(value, '\x00') {
		return fmt.Errorf("%s name %q contains NUL", kind, value)
	}
	return nil
}

func lowerFirst(value string) string {
	if value == "" {
		return value
	}
	runes := []rune(value)
	runes[0] = unicode.ToLower(runes[0])
	return string(runes)
}

func snakeCase(value string) string {
	runes := []rune(value)
	var builder strings.Builder
	for index, current := range runes {
		if unicode.IsUpper(current) && index > 0 {
			previous := runes[index-1]
			nextLower := index+1 < len(runes) && unicode.IsLower(runes[index+1])
			if unicode.IsLower(previous) || unicode.IsDigit(previous) || unicode.IsUpper(previous) && nextLower {
				builder.WriteByte('_')
			}
		}
		builder.WriteRune(unicode.ToLower(current))
	}
	return builder.String()
}

func diagnostic(position token.Position, format string, arguments ...any) error {
	message := fmt.Sprintf(format, arguments...)
	if position.IsValid() {
		return fmt.Errorf("ormgen: %s: %s", position, message)
	}
	return fmt.Errorf("ormgen: %s", message)
}
