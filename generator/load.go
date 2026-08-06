package generator

import (
	"bytes"
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
)

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
					switch typed := spec.(type) {
					case *ast.TypeSpec:
						declarations[typed.Name.Name] = fileSet.Position(typed.Pos())
						if _, ok := typed.Type.(*ast.StructType); ok {
							structs[typed.Name.Name] = struct {
								typeSpec *ast.TypeSpec
								genDecl  *ast.GenDecl
								file     sourceFile
							}{typeSpec: typed, genDecl: value, file: file}
						}
					case *ast.ValueSpec:
						for _, name := range typed.Names {
							declarations[name.Name] = fileSet.Position(name.Pos())
						}
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
		generatedNames := []string{
			entity.name + "ORM",
			entity.name + "ORMMetadata",
			"new" + entity.name + "ORMMetadata",
			"must" + entity.name + "ORMTable",
			"must" + entity.name + "ORMColumn",
			"scan" + entity.name + "ORM",
		}
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
		if name == "Table" {
			return entityModel{}, nil, diagnostic(position, "field %s.Table conflicts with generated metadata field Table", entity.name)
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
		for alias := range selectorAliases(field.Type) {
			imported, exists := file.imports[alias]
			if !exists {
				return entityModel{}, nil, diagnostic(position, "cannot resolve package alias %q in field type; use an explicit import alias", alias)
			}
			usedImports[alias] = imported
		}
		entity.fields = append(entity.fields, fieldModel{
			name:       name,
			columnName: columnName,
			typeText:   typeText,
			capability: mutationCapability(options),
			position:   position,
		})
	}
	if len(entity.fields) == 0 {
		return entityModel{}, nil, diagnostic(entity.position, "entity %s has no selectable exported fields", entity.name)
	}
	return entity, usedImports, nil
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

func formatNode(fileSet *token.FileSet, node ast.Node) (string, error) {
	var buffer bytes.Buffer
	if err := format.Node(&buffer, fileSet, node); err != nil {
		return "", err
	}
	return buffer.String(), nil
}
