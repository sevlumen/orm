package apicheck

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateIsDeterministicAndExcludesInternalPackages(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	first, err := Generate(root)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Generate(root)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("API surface generation is non-deterministic")
	}
	text := string(first)
	if !strings.Contains(text, "package github.com/sevlumen/orm\n") {
		t.Fatalf("root package missing from API surface:\n%s", text)
	}
	if strings.Contains(text, "github.com/sevlumen/orm/internal/") || strings.Contains(text, "github.com/sevlumen/orm/cmd/") {
		t.Fatalf("private package leaked into API surface:\n%s", text)
	}
}

func TestReadModulePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "go.mod")
	if err := os.WriteFile(path, []byte("module example.test/module\n\ngo 1.25\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readModulePath(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != "example.test/module" {
		t.Fatalf("module path=%q", got)
	}
}
