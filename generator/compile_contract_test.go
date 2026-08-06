package generator

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenamedEntityFieldBreaksStaleGeneratedCode(t *testing.T) {
	t.Parallel()
	dir := newGeneratorModule(t, `package sample

type User struct {
    ID int64
    Email string
}
`)
	config := Config{Dir: dir, Types: []string{"User"}, Output: "orm_gen.go"}
	if err := Write(config); err != nil {
		t.Fatal(err)
	}
	runGoTest(t, dir)

	renamed := `package sample

type User struct {
    ID int64
    Address string
}
`
	if err := os.WriteFile(filepath.Join(dir, "entity.go"), []byte(renamed), 0o644); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "test", "./...")
	command.Dir = dir
	command.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local")
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatal("stale generated code unexpectedly compiled after field rename")
	}
	text := string(output)
	if !strings.Contains(text, "Email") {
		t.Fatalf("compile error does not identify stale field:\n%s", text)
	}
	if err := Write(Config{Dir: dir, Types: []string{"User"}, Output: "orm_gen.go", Check: true}); err == nil {
		t.Fatal("check mode did not detect stale generated code")
	}
	if err := Write(config); err != nil {
		t.Fatal(err)
	}
	runGoTest(t, dir)
}

func TestGeneratedDeclarationAndTableFieldConflictsFailBeforeWrite(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "reserved table field",
			source: `package sample

type User struct { Table string }
`,
			want: "conflicts with generated metadata field Table",
		},
		{
			name: "existing generated variable",
			source: `package sample

type User struct { ID int64 }
var UserORM = 1
`,
			want: "generated declaration UserORM conflicts",
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
		})
	}
}
