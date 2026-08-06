package generator

import (
	"fmt"
	"go/ast"
	"go/token"
	"strconv"
	"strings"
	"unicode"
)

func tableDirective(groups ...*ast.CommentGroup) (string, bool, error) {
	var value string
	found := false
	for _, group := range groups {
		if group == nil {
			continue
		}
		for _, comment := range group.List {
			text := strings.TrimSpace(strings.TrimPrefix(comment.Text, "//"))
			if text != "orm:table" && !strings.HasPrefix(text, "orm:table ") && !strings.HasPrefix(text, "orm:table\t") {
				continue
			}
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
	return structTag(value).get("orm")
}

type structTag string

func (tag structTag) get(key string) (string, error) {
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
	if result["notNull"] == "true" && result["nullable"] == "true" {
		return nil, fmt.Errorf("notNull and nullable cannot be used together")
	}
	if result["default"] != "" && result["generated"] != "" {
		return nil, fmt.Errorf("default and generated cannot be used together")
	}
	capabilities := make([]string, 0, 3)
	for _, name := range []string{"readOnly", "insertOnly", "updateOnly"} {
		if result[name] == "true" {
			capabilities = append(capabilities, name)
		}
	}
	if len(capabilities) > 1 {
		return nil, fmt.Errorf("mutation capability tags %s cannot be combined", strings.Join(capabilities, ", "))
	}
	if result["generated"] != "" && len(capabilities) == 1 && capabilities[0] != "readOnly" {
		return nil, fmt.Errorf("generated columns must be read-only")
	}
	return result, nil
}

func mutationCapability(options map[string]string) string {
	if options["generated"] != "" || options["readOnly"] == "true" {
		return "readOnly"
	}
	if options["insertOnly"] == "true" {
		return "insertOnly"
	}
	if options["updateOnly"] == "true" {
		return "updateOnly"
	}
	return "mutable"
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
