package generator

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const validEntitySource = `package sample

import (
    "encoding/json"
    tm "time"
)

type Status string

// orm:table app_users
type User struct {
    ID int64 ` + "`orm:\"column:user_id;insertOnly;primaryKey\"`" + `
    Email *string
    CreatedAt tm.Time ` + "`orm:\"column:created_at;readOnly\"`" + `
    Payload json.RawMessage ` + "`orm:\"readOnly\"`" + `
    Tags []string
    Metadata map[string]any
    Status Status
    Search string ` + "`orm:\"generated:email;readOnly\"`" + `
    Ignored string ` + "`orm:\"-\"`" + `
    internal string
}
`

func TestGenerateDeterministicTypedMetadataThatCompiles(t *testing.T) {
	t.Parallel()
	dir := newGeneratorModule(t, validEntitySource)
	config := Config{Dir: dir, Types: []string{"User"}, Output: "orm_gen.go"}

	first, err := Generate(config)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Generate(config)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("generated output is not deterministic")
	}
	text := string(first)
	checks := []string{
		"var UserORM = newUserORMMetadata()",
		"Table *ormquery.Table[User]",
		"ID ormquery.Column[User, int64]",
		"Email ormquery.Column[User, *string]",
		"CreatedAt ormquery.Column[User, tm.Time]",
		"Payload ormquery.Column[User, json.RawMessage]",
		"Tags ormquery.Column[User, []string]",
		"Metadata ormquery.Column[User, map[string]any]",
		"ormquery.InsertOnlyColumn()",
		"ormquery.ReadOnlyColumn()",
		`NewTable[User]("app_users"`,
		"row.Scan(&value.ID, &value.Email, &value.CreatedAt, &value.Payload, &value.Tags, &value.Metadata, &value.Status, &value.Search)",
	}
	for _, expected := range checks {
		if !strings.Contains(text, expected) {
			t.Fatalf("generated output does not contain %q:\n%s", expected, text)
		}
	}
	if strings.Contains(text, "Ignored") || strings.Contains(text, "internal") || strings.Contains(text, "reflect") {
		t.Fatalf("generated output contains ignored or reflective content:\n%s", text)
	}

	if err := Write(config); err != nil {
		t.Fatal(err)
	}
	if err := Write(Config{Dir: dir, Types: []string{"User"}, Output: "orm_gen.go", Check: true}); err != nil {
		t.Fatal(err)
	}
	runGoTest(t, dir)
}

func TestCheckModeDetectsStaleOutputWithoutWriting(t *testing.T) {
	t.Parallel()
	dir := newGeneratorModule(t, validEntitySource)
	output := filepath.Join(dir, "orm_gen.go")
	original := []byte("package sample\n")
	if err := os.WriteFile(output, original, 0o644); err != nil {
		t.Fatal(err)
	}
	err := Write(Config{Dir: dir, Types: []string{"User"}, Output: "orm_gen.go", Check: true})
	var stale *StaleError
	if !errors.As(err, &stale) {
		t.Fatalf("error = %v, want StaleError", err)
	}
	current, readErr := os.ReadFile(output)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(current, original) {
		t.Fatal("check mode modified the output file")
	}
}

func TestGenerateRejectsUnsafeEntityShapesWithDiagnostics(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "embedded field",
			source: `package sample

type Base struct { ID int64 }
type User struct { Base }
`,
			want: "embedded field",
		},
		{
			name: "duplicate columns",
			source: "package sample\n\ntype User struct {\n A string `orm:\"column:value\"`\n B string `orm:\"column:value\"`\n}\n",
			want: "duplicates field",
		},
		{
			name: "dynamic table name",
			source: `package sample

type User struct { ID int64 }
func (User) TableName() string { name := "users"; return name }
`,
			want: "string literal",
		},
		{
			name: "unexported tagged field",
			source: "package sample\n\ntype User struct { secret string `orm:\"column:secret\"` }\n",
			want: "unexported field",
		},
		{
			name: "capability conflict",
			source: "package sample\n\ntype User struct { ID int64 `orm:\"insertOnly;updateOnly\"` }\n",
			want: "cannot be combined",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dir := newGeneratorModule(t, test.source)
			_, err := Generate(Config{Dir: dir, Types: []string{"User"}})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
			if !strings.Contains(err.Error(), ".go:") {
				t.Fatalf("error lacks file/line diagnostic: %v", err)
			}
		})
	}
}

func TestWritePreservesExistingOutputOnGenerationFailure(t *testing.T) {
	t.Parallel()
	dir := newGeneratorModule(t, "package sample\n\ntype User struct { Embedded }\ntype Embedded struct{}\n")
	output := filepath.Join(dir, "orm_gen.go")
	original := []byte("package sample\n\nconst Preserved = true\n")
	if err := os.WriteFile(output, original, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "entity.go"), []byte("package sample\n\ntype User struct { Embedded }\ntype Embedded struct{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Write(Config{Dir: dir, Types: []string{"User"}, Output: "orm_gen.go"}); err == nil {
		t.Fatal("expected generation failure")
	}
	current, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(current, original) {
		t.Fatal("failed generation modified existing output")
	}
}

func newGeneratorModule(t *testing.T, source string) string {
	t.Helper()
	dir := t.TempDir()
	root := repositoryRoot(t)
	goMod := "module example.com/generated\n\ngo 1.25.0\n\nrequire github.com/sevlumen/orm v0.0.0\n\nreplace github.com/sevlumen/orm => " + filepath.ToSlash(root) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "entity.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate generator test file")
	}
	return filepath.Dir(filepath.Dir(filename))
}

func runGoTest(t *testing.T, dir string) {
	t.Helper()
	command := exec.Command("go", "test", "./...")
	command.Dir = dir
	command.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated package does not compile: %v\n%s", err, output)
	}
}
