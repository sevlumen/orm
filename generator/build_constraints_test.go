package generator

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGenerateRespectsBuildConstraintsAndPlatformSuffixes(t *testing.T) {
	t.Parallel()
	dir := newGeneratorModule(t, `package sample

type User struct { ID int64 }
`)
	ignoredByTag := `//go:build ormgen_never

package sample

type User struct { Wrong string }
`
	if err := os.WriteFile(filepath.Join(dir, "entity_ignored.go"), []byte(ignoredByTag), 0o644); err != nil {
		t.Fatal(err)
	}

	otherPlatform := "windows"
	if runtime.GOOS == "windows" {
		otherPlatform = "linux"
	}
	ignoredByPlatform := `package sample

type User struct { PlatformWrong string }
`
	if err := os.WriteFile(filepath.Join(dir, "entity_"+otherPlatform+".go"), []byte(ignoredByPlatform), 0o644); err != nil {
		t.Fatal(err)
	}

	generated, err := Generate(Config{Dir: dir, Types: []string{"User"}})
	if err != nil {
		t.Fatal(err)
	}
	text := string(generated)
	if !strings.Contains(text, "&value.ID") {
		t.Fatalf("active entity field missing:\n%s", text)
	}
	if strings.Contains(text, "Wrong") {
		t.Fatalf("inactive build-constraint field leaked into output:\n%s", text)
	}
}

func TestGenerateRejectsDotImports(t *testing.T) {
	t.Parallel()
	dir := newGeneratorModule(t, `package sample

import . "time"

type User struct { CreatedAt Time }
`)
	_, err := Generate(Config{Dir: dir, Types: []string{"User"}})
	if err == nil || !strings.Contains(err.Error(), "dot imports are not supported") {
		t.Fatalf("error = %v, want dot import diagnostic", err)
	}
}
