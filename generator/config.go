// Package generator creates typed query metadata and direct row scanners from Go structs.
package generator

import (
	"bytes"
	"errors"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"strings"
)

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
	_, output, err := resolvePaths(config)
	if err != nil {
		return err
	}
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
	if err := replaceFile(temporaryName, output); err != nil {
		return fmt.Errorf("ormgen: replace generated file: %w", err)
	}
	return nil
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
