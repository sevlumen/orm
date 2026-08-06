package ormcli

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/sevlumen/orm/generator"
)

const generateUsage = `Usage: orm generate [options]

Options:
  --config <path>   Versioned orm JSON config file.
  --dir <path>      Entity package directory.
  --output <path>   Generated Go file inside the package.
  --type <name>     Exported entity type; repeat or comma-separate.
  --check           Verify generated output without writing.
  --json            Emit a versioned JSON result.
`

type generateResult struct {
	Directory string   `json:"directory"`
	Output    string   `json:"output"`
	Types     []string `json:"types"`
	Check     bool     `json:"check"`
}

func (app *App) runGenerate(_ context.Context, args []string) error {
	var configPath string
	var directory optionalString
	var output optionalString
	var types stringList
	var check bool
	var jsonOutput bool
	_, err := parseFlags("generate", generateUsage, args, func(set *flag.FlagSet) {
		set.StringVar(&configPath, "config", "", "versioned orm JSON config file")
		set.Var(&directory, "dir", "entity package directory")
		set.Var(&output, "output", "generated Go file")
		set.Var(&types, "type", "exported entity type")
		set.BoolVar(&check, "check", false, "verify generated output")
		set.BoolVar(&jsonOutput, "json", false, "emit JSON")
	})
	if err != nil {
		return err
	}
	config, err := loadConfig(configPath)
	if err != nil {
		return &usageError{message: err.Error(), usage: generateUsage, cause: err}
	}
	resolvedDirectory := config.Generate.Directory
	if directory.set {
		resolvedDirectory = directory.value
	}
	resolvedOutput := config.Generate.Output
	if output.set {
		resolvedOutput = output.value
	}
	resolvedTypes := append([]string(nil), config.Generate.Types...)
	if len(types) > 0 {
		resolvedTypes = append([]string(nil), types...)
	}
	if len(resolvedTypes) == 0 {
		return &usageError{message: "at least one --type or generate.types entry is required", usage: generateUsage}
	}
	if err := generator.Write(generator.Config{
		Dir:    resolvedDirectory,
		Output: resolvedOutput,
		Types:  resolvedTypes,
		Check:  check,
	}); err != nil {
		return err
	}
	result := generateResult{
		Directory: resolvedDirectory,
		Output:    resolvedOutput,
		Types:     append([]string(nil), resolvedTypes...),
		Check:     check,
	}
	humanAction := "generated"
	if check {
		humanAction = "verified"
	}
	human := fmt.Sprintf("%s %s for %s\n", humanAction, resolvedOutput, strings.Join(resolvedTypes, ", "))
	return app.writeResult(jsonOutput, "generate", result, human)
}
