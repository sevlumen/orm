package query

import (
	"fmt"
	"strings"
)

type renderContext struct {
	args []any
}

func (r *renderContext) bind(value any) string {
	r.args = append(r.args, value)
	return fmt.Sprintf("$%d", len(r.args))
}

func (r *renderContext) statement(sql string) Statement {
	args := append([]any(nil), r.args...)
	return Statement{SQL: sql, Args: args}
}

func validateTrustedSQL(value TrustedSQL) error {
	text := string(value)
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("query: trusted SQL expression is empty")
	}
	if strings.ContainsRune(text, '\x00') {
		return fmt.Errorf("query: trusted SQL expression contains NUL")
	}
	return nil
}

func joinErrors(errors ...error) error {
	for _, err := range errors {
		if err != nil {
			return err
		}
	}
	return nil
}
