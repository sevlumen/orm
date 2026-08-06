package schema

import (
	"fmt"
	"strings"
)

// Extension declares a PostgreSQL extension required by the schema.
type Extension struct {
	Name string `json:"name"`
}

// EnumType declares a PostgreSQL enum type and its ordered labels.
type EnumType struct {
	Name   string   `json:"name"`
	Values []string `json:"values"`
}

func validateNativeSchema(model Schema, relationNames map[string]string) error {
	seenExtensions := map[string]struct{}{}
	for _, extension := range model.Extensions {
		if err := validateIdentifier("extension", extension.Name); err != nil {
			return err
		}
		if _, exists := seenExtensions[extension.Name]; exists {
			return fmt.Errorf("schema: duplicate extension %q", extension.Name)
		}
		seenExtensions[extension.Name] = struct{}{}
	}

	seenEnums := map[string]struct{}{}
	for _, enum := range model.Enums {
		if err := validateIdentifier("enum", enum.Name); err != nil {
			return err
		}
		if _, exists := seenEnums[enum.Name]; exists {
			return fmt.Errorf("schema: duplicate enum %q", enum.Name)
		}
		seenEnums[enum.Name] = struct{}{}
		if previous, exists := relationNames[enum.Name]; exists {
			return fmt.Errorf("schema: enum %q conflicts with %s", enum.Name, previous)
		}
		relationNames[enum.Name] = "another type or relation"
		if len(enum.Values) == 0 {
			return fmt.Errorf("schema: enum %q has no values", enum.Name)
		}
		seenValues := map[string]struct{}{}
		for _, value := range enum.Values {
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("schema: enum %q has an empty value", enum.Name)
			}
			if strings.ContainsRune(value, '\x00') {
				return fmt.Errorf("schema: enum %q value contains NUL", enum.Name)
			}
			if _, exists := seenValues[value]; exists {
				return fmt.Errorf("schema: enum %q repeats value %q", enum.Name, value)
			}
			seenValues[value] = struct{}{}
		}
	}
	return nil
}
