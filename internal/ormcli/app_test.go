package ormcli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAppHelpAndUsageExitCodes(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		args []string
		want int
	}{
		{"root help", []string{"--help"}, 0},
		{"missing command", nil, 2},
		{"unknown command", []string{"unknown"}, 2},
		{"unknown flag", []string{"generate", "--unknown"}, 2},
		{"command help", []string{"status", "--help"}, 0},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			app := New()
			app.Out, app.Err = &stdout, &stderr
			if exit := app.Run(context.Background(), test.args); exit != test.want {
				t.Fatalf("exit = %d, want %d; stdout=%q stderr=%q", exit, test.want, stdout.String(), stderr.String())
			}
		})
	}
}

func TestGenerateCommandWritesAndChecksDeterministicOutput(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	entity := `package sample

type User struct {
    ID int64
    Email string
}
`
	if err := os.WriteFile(filepath.Join(directory, "entity.go"), []byte(entity), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	app := New()
	app.Out, app.Err = &stdout, &stderr
	exit := app.Run(context.Background(), []string{
		"generate", "--dir", directory, "--output", "orm_gen.go", "--type", "User", "--json",
	})
	if exit != 0 {
		t.Fatalf("generate exit = %d, stderr=%s", exit, stderr.String())
	}
	generated, err := os.ReadFile(filepath.Join(directory, "orm_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(generated), "var UserORM") || !strings.Contains(stdout.String(), `"command":"generate"`) {
		t.Fatalf("unexpected output: generated=%s stdout=%s", generated, stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	exit = app.Run(context.Background(), []string{
		"generate", "--dir", directory, "--output", "orm_gen.go", "--type", "User", "--check",
	})
	if exit != 0 {
		t.Fatalf("check exit = %d, stderr=%s", exit, stderr.String())
	}
}

func TestDatabaseErrorsRedactURLAndPassword(t *testing.T) {
	t.Parallel()
	const databaseURL = "postgres://user:super-secret@example.invalid/db?sslmode=disable"
	var stdout, stderr bytes.Buffer
	app := New()
	app.Out, app.Err = &stdout, &stderr
	app.OpenPool = func(context.Context, string) (*pgxpool.Pool, error) {
		return nil, errors.New("connection failed for " + databaseURL + " password super-secret")
	}
	exit := app.Run(context.Background(), []string{"status", "--database-url", databaseURL})
	if exit != 1 {
		t.Fatalf("exit = %d, want 1", exit)
	}
	message := stderr.String()
	if strings.Contains(message, databaseURL) || strings.Contains(message, "super-secret") {
		t.Fatalf("stderr leaked database secret: %s", message)
	}
	if !strings.Contains(message, "[REDACTED]") {
		t.Fatalf("stderr does not show redaction marker: %s", message)
	}
}

func TestCanceledContextReachesDatabaseCommandBoundary(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var stderr bytes.Buffer
	app := New()
	app.Out, app.Err = bytes.NewBuffer(nil), &stderr
	app.OpenPool = func(ctx context.Context, _ string) (*pgxpool.Pool, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	exit := app.Run(ctx, []string{"status", "--database-url", "postgres://localhost/test"})
	if exit != 1 {
		t.Fatalf("exit = %d, want 1; stderr=%s", exit, stderr.String())
	}
}
