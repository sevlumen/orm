package ormcli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sevlumen/orm/migration"
	"github.com/sevlumen/orm/migration/artifact"
	"github.com/sevlumen/orm/schema"
)

func TestDiffAndValidateArtifactWorkflow(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	migrationsDirectory := filepath.Join(root, "migrations")
	firstSnapshot := mustSnapshot(t, schema.Schema{Tables: []schema.Table{{
		Name: "users",
		Columns: []schema.Column{
			{Name: "id", Type: "bigint", PrimaryKey: true},
			{Name: "email", Type: "text"},
		},
	}}})
	firstPath := writeSnapshot(t, root, "first.json", firstSnapshot)

	var stdout, stderr bytes.Buffer
	app := New()
	app.Out, app.Err = &stdout, &stderr
	firstID := "202608060001_init"
	exit := app.Run(context.Background(), []string{
		"diff", "--after", firstPath, "--id", firstID,
		"--migrations", migrationsDirectory, "--max-risk", "safe", "--json",
	})
	if exit != 0 {
		t.Fatalf("first diff exit=%d stderr=%s", exit, stderr.String())
	}
	if _, err := artifact.Load(migrationsDirectory, firstID); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"risk":"safe"`) {
		t.Fatalf("unexpected diff JSON: %s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	exit = app.Run(context.Background(), []string{"validate", "--migrations", migrationsDirectory, "--json"})
	if exit != 0 || !strings.Contains(stdout.String(), `"artifacts":1`) {
		t.Fatalf("validate exit=%d stdout=%s stderr=%s", exit, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	exit = app.Run(context.Background(), []string{
		"diff", "--after", firstPath, "--id", "202608060002_noop", "--migrations", migrationsDirectory,
	})
	if exit != 1 || !strings.Contains(stderr.String(), "no schema changes") {
		t.Fatalf("noop exit=%d stderr=%s", exit, stderr.String())
	}

	secondSnapshot := mustSnapshot(t, schema.Schema{Tables: []schema.Table{{
		Name: "users",
		Columns: []schema.Column{
			{Name: "id", Type: "bigint", PrimaryKey: true},
			{Name: "email", Type: "text"},
		},
		UniqueConstraints: []schema.UniqueConstraint{{Name: "uq_users_email", Columns: []string{"email"}}},
	}}})
	secondPath := writeSnapshot(t, root, "second.json", secondSnapshot)
	secondID := "202608060002_unique"

	stderr.Reset()
	exit = app.Run(context.Background(), []string{
		"diff", "--after", secondPath, "--id", secondID,
		"--migrations", migrationsDirectory, "--max-risk", "safe",
	})
	if exit != 1 || !strings.Contains(stderr.String(), "exceeds allowed maximum") {
		t.Fatalf("risk refusal exit=%d stderr=%s", exit, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	exit = app.Run(context.Background(), []string{
		"diff", "--after", secondPath, "--id", secondID,
		"--migrations", migrationsDirectory, "--max-risk", "review",
	})
	if exit != 0 {
		t.Fatalf("review diff exit=%d stderr=%s", exit, stderr.String())
	}

	stderr.Reset()
	exit = app.Run(context.Background(), []string{
		"diff", "--after", secondPath, "--id", firstID,
		"--migrations", migrationsDirectory, "--max-risk", "review",
	})
	if exit != 1 || !strings.Contains(stderr.String(), "must sort after") {
		t.Fatalf("non-monotonic exit=%d stderr=%s", exit, stderr.String())
	}

	emptyPath := writeSnapshot(t, root, "empty.json", migration.EmptySnapshot())
	stderr.Reset()
	exit = app.Run(context.Background(), []string{
		"diff", "--before", emptyPath, "--after", secondPath,
		"--id", "202608060003_divergent", "--migrations", migrationsDirectory,
		"--max-risk", "destructive",
	})
	if exit != 1 || !strings.Contains(stderr.String(), "does not match latest") {
		t.Fatalf("divergent before exit=%d stderr=%s", exit, stderr.String())
	}

	upPath := filepath.Join(migrationsDirectory, secondID, artifact.UpFilename)
	if err := os.WriteFile(upPath, []byte("SELECT 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stderr.Reset()
	exit = app.Run(context.Background(), []string{"validate", "--migrations", migrationsDirectory})
	if exit != 1 || !strings.Contains(stderr.String(), "checksum") {
		t.Fatalf("checksum validation exit=%d stderr=%s", exit, stderr.String())
	}
}

func TestValidateSnapshotAndConflictingInputs(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := writeSnapshot(t, root, "snapshot.json", migration.EmptySnapshot())
	var stdout, stderr bytes.Buffer
	app := New()
	app.Out, app.Err = &stdout, &stderr
	if exit := app.Run(context.Background(), []string{"validate", "--snapshot", path, "--json"}); exit != 0 {
		t.Fatalf("snapshot validate exit=%d stderr=%s", exit, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"snapshotValid":true`) {
		t.Fatalf("unexpected JSON: %s", stdout.String())
	}
	stderr.Reset()
	if exit := app.Run(context.Background(), []string{"validate", "--snapshot", path, "--migrations", root}); exit != 2 {
		t.Fatalf("conflict exit=%d stderr=%s", exit, stderr.String())
	}
}

func mustSnapshot(t *testing.T, model schema.Schema) migration.Snapshot {
	t.Helper()
	snapshot, err := migration.NewSnapshot(model)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func writeSnapshot(t *testing.T, directory, name string, snapshot migration.Snapshot) string {
	t.Helper()
	data, err := snapshot.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
