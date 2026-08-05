package runner

import (
	"fmt"
	"strings"
	"unicode"
)

var prohibitedTransactionStatements = map[string]struct{}{
	"ABORT":     {},
	"BEGIN":     {},
	"COMMIT":    {},
	"END":       {},
	"ROLLBACK":  {},
	"SAVEPOINT": {},
}

// validateMigrationScript rejects transaction-control commands that could
// escape the transaction managed by Runner. It tokenizes only statement
// prefixes while correctly skipping PostgreSQL comments, strings, quoted
// identifiers, and dollar-quoted bodies.
func validateMigrationScript(script string) error {
	tokens, err := statementPrefixes(script, 2)
	if err != nil {
		return err
	}
	for _, prefix := range tokens {
		if len(prefix) == 0 {
			continue
		}
		first := strings.ToUpper(prefix[0])
		if _, prohibited := prohibitedTransactionStatements[first]; prohibited {
			return fmt.Errorf("runner: transaction-control statement %q is not allowed in migration SQL", strings.Join(prefix, " "))
		}
		if first == "COPY" {
			return fmt.Errorf("runner: COPY statements are not supported in migration SQL")
		}
		if first == "START" && len(prefix) > 1 && strings.EqualFold(prefix[1], "TRANSACTION") {
			return fmt.Errorf("runner: transaction-control statement %q is not allowed in migration SQL", strings.Join(prefix, " "))
		}
		if first == "PREPARE" && len(prefix) > 1 && strings.EqualFold(prefix[1], "TRANSACTION") {
			return fmt.Errorf("runner: transaction-control statement %q is not allowed in migration SQL", strings.Join(prefix, " "))
		}
		if first == "RELEASE" && len(prefix) > 1 && strings.EqualFold(prefix[1], "SAVEPOINT") {
			return fmt.Errorf("runner: transaction-control statement %q is not allowed in migration SQL", strings.Join(prefix, " "))
		}
	}
	return nil
}

func statementPrefixes(input string, maximum int) ([][]string, error) {
	var result [][]string
	var current []string
	for i := 0; i < len(input); {
		switch {
		case isSpace(input[i]):
			i++
		case input[i] == ';':
			result = appendPrefix(result, current)
			current = nil
			i++
		case input[i] == '-' && i+1 < len(input) && input[i+1] == '-':
			i += 2
			for i < len(input) && input[i] != '\n' {
				i++
			}
		case input[i] == '/' && i+1 < len(input) && input[i+1] == '*':
			var err error
			i, err = skipBlockComment(input, i)
			if err != nil {
				return nil, err
			}
		case input[i] == '\'':
			var err error
			i, err = skipSingleQuoted(input, i, isEscapeStringPrefix(input, i))
			if err != nil {
				return nil, err
			}
		case input[i] == '"':
			var err error
			i, err = skipDoubleQuoted(input, i)
			if err != nil {
				return nil, err
			}
		case input[i] == '$':
			end, ok, err := skipDollarQuoted(input, i)
			if err != nil {
				return nil, err
			}
			if ok {
				i = end
				continue
			}
			fallthrough
		default:
			start := i
			for i < len(input) && isIdentifierPart(input[i]) {
				i++
			}
			if start == i {
				i++
				continue
			}
			if len(current) < maximum {
				current = append(current, input[start:i])
			}
		}
	}
	result = appendPrefix(result, current)
	return result, nil
}

func appendPrefix(result [][]string, prefix []string) [][]string {
	if len(prefix) == 0 {
		return result
	}
	copyPrefix := append([]string(nil), prefix...)
	return append(result, copyPrefix)
}

func skipBlockComment(input string, start int) (int, error) {
	depth := 1
	for i := start + 2; i < len(input); {
		switch {
		case i+1 < len(input) && input[i] == '/' && input[i+1] == '*':
			depth++
			i += 2
		case i+1 < len(input) && input[i] == '*' && input[i+1] == '/':
			depth--
			i += 2
			if depth == 0 {
				return i, nil
			}
		default:
			i++
		}
	}
	return 0, fmt.Errorf("runner: unterminated block comment in migration SQL")
}

func skipSingleQuoted(input string, start int, backslashEscapes bool) (int, error) {
	for i := start + 1; i < len(input); i++ {
		if backslashEscapes && input[i] == '\\' && i+1 < len(input) {
			i++
			continue
		}
		if input[i] != '\'' {
			continue
		}
		if i+1 < len(input) && input[i+1] == '\'' {
			i++
			continue
		}
		return i + 1, nil
	}
	return 0, fmt.Errorf("runner: unterminated string literal in migration SQL")
}

func isEscapeStringPrefix(input string, quote int) bool {
	if quote == 0 || input[quote-1] != 'E' && input[quote-1] != 'e' {
		return false
	}
	return quote == 1 || !isIdentifierPart(input[quote-2])
}

func skipDoubleQuoted(input string, start int) (int, error) {
	for i := start + 1; i < len(input); i++ {
		if input[i] != '"' {
			continue
		}
		if i+1 < len(input) && input[i+1] == '"' {
			i++
			continue
		}
		return i + 1, nil
	}
	return 0, fmt.Errorf("runner: unterminated quoted identifier in migration SQL")
}

func skipDollarQuoted(input string, start int) (int, bool, error) {
	i := start + 1
	for i < len(input) && (unicode.IsLetter(rune(input[i])) || unicode.IsDigit(rune(input[i])) || input[i] == '_') {
		i++
	}
	if i >= len(input) || input[i] != '$' {
		return start, false, nil
	}
	delimiter := input[start : i+1]
	end := strings.Index(input[i+1:], delimiter)
	if end < 0 {
		return 0, false, fmt.Errorf("runner: unterminated dollar-quoted string in migration SQL")
	}
	return i + 1 + end + len(delimiter), true, nil
}

func isSpace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\n' || value == '\r' || value == '\f'
}

func isIdentifierPart(value byte) bool {
	return value == '_' || value == '$' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9'
}
