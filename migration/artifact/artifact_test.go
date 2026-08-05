package artifact

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/sevlumen/orm/migration"
	"github.com/sevlumen/orm/postgres"
	"github.com/sevlumen/orm/schema"
)

func testArtifact(t *testing.T, id string) Artifact {
	t.Helper()
	snapshot, err := migration.NewSnapshot(schema.Schema{Tables: []schema.Table{{
		Name:    "users",
		Columns: []schema.Column{{Name: "id", Type: "uuid", PrimaryKey: true}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := Build(id, postgres.MigrationSQL{
		Up:   "CREATE TABLE users (id uuid PRIMARY KEY);\n",
		Down: "DROP TABLE users;\n",
		Risk: migration.RiskSafe,
	}, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestBuildIsDeterministic(t *testing.T) {
	t.Parallel()
	first := testArtifact(t, "20260805210000_create_users")
	second := testArtifact(t, "20260805210000_create_users")
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("artifacts differ\nfirst: %#v\nsecond: %#v", first, second)
	}
	firstManifest, err := first.MarshalManifest()
	if err != nil {
		t.Fatal(err)
	}
	secondManifest, err := second.MarshalManifest()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstManifest, secondManifest) {
		t.Fatal("manifest output is not deterministic")
	}
}

func TestValidateIDRejectsTraversal(t *testing.T) {
	t.Parallel()
	invalid := []string{"../escape", "20260805210000_../escape", "20260805210000_CreateUsers", "create_users", "20261301210000_invalid_month", "20260805210000_" + strings.Repeat("a", maxMigrationIDLength)}
	for _, id := range invalid {
		if err := ValidateID(id); err == nil {
			t.Fatalf("ValidateID(%q) unexpectedly succeeded", id)
		}
	}
}

func TestWriteLoadAndList(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	first := testArtifact(t, "20260805210000_create_users")
	second := testArtifact(t, "20260805220000_add_roles")
	firstPath, err := Write(root, first)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(firstPath) != first.Manifest.ID {
		t.Fatalf("path = %q", firstPath)
	}
	if _, err := Write(root, second); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(root, first.Manifest.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, loaded) {
		t.Fatalf("loaded artifact differs: %#v", loaded)
	}
	ids, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{first.Manifest.ID, second.Manifest.ID}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("List() = %v, want %v", ids, want)
	}
}

func TestWriteRejectsDuplicateWithoutChangingOriginal(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	original := testArtifact(t, "20260805210000_create_users")
	if _, err := Write(root, original); err != nil {
		t.Fatal(err)
	}
	modified := original
	modified.UpSQL = []byte("SELECT 1;\n")
	modified.Manifest.Files.UpSQL = digest(modified.UpSQL)
	if _, err := Write(root, modified); err == nil {
		t.Fatal("expected duplicate migration error")
	}
	loaded, err := Load(root, original.Manifest.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(loaded.UpSQL, original.UpSQL) {
		t.Fatal("duplicate write changed the original migration")
	}
}

func TestLoadDetectsTampering(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	artifact := testArtifact(t, "20260805210000_create_users")
	path, err := Write(root, artifact)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, UpFile), []byte("DROP DATABASE production;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root, artifact.Manifest.ID); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected checksum mismatch, got %v", err)
	}
}

func TestWriterCleansPartialFilesAfterFailure(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	artifact := testArtifact(t, "20260805210000_create_users")
	writes := 0
	writer := filesystemWriter{writeFile: func(path string, data []byte, mode os.FileMode) error {
		writes++
		if writes == 2 {
			return errors.New("injected failure")
		}
		return writeFileSync(path, data, mode)
	}}
	if _, err := writer.write(root, artifact); err == nil {
		t.Fatal("expected injected write failure")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("partial entries remain: %v", entries)
	}
}

func TestConcurrentWritersDoNotOverwrite(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	value := testArtifact(t, "20260805210000_create_users")
	results := make(chan error, 2)
	var start sync.WaitGroup
	start.Add(1)
	for range 2 {
		go func() {
			start.Wait()
			_, err := Write(root, value)
			results <- err
		}()
	}
	start.Done()

	successes := 0
	failures := 0
	for range 2 {
		if err := <-results; err != nil {
			failures++
		} else {
			successes++
		}
	}
	if successes != 1 || failures != 1 {
		t.Fatalf("successes = %d, failures = %d", successes, failures)
	}
	if _, err := Load(root, value.Manifest.ID); err != nil {
		t.Fatalf("published artifact is invalid: %v", err)
	}
}

func TestListRequiresRoot(t *testing.T) {
	t.Parallel()
	if _, err := List(""); err == nil {
		t.Fatal("expected empty root error")
	}
}

func TestArtifactRejectsOversizedSQL(t *testing.T) {
	t.Parallel()
	value := testArtifact(t, "20260805210000_create_users")
	value.UpSQL = bytes.Repeat([]byte{'x'}, maxSQLSize+1)
	value.Manifest.Files.UpSQL = digest(value.UpSQL)
	if err := value.Validate(); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected size error, got %v", err)
	}
}

func TestLoadRejectsSymlinkPayload(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation commonly requires elevated Windows privileges")
	}
	root := t.TempDir()
	value := testArtifact(t, "20260805210000_create_users")
	path, err := Write(root, value)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "external.sql")
	if err := os.WriteFile(target, value.UpSQL, 0o644); err != nil {
		t.Fatal(err)
	}
	payload := filepath.Join(path, UpFile)
	if err := os.Remove(payload); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, payload); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root, value.Manifest.ID); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("expected symlink rejection, got %v", err)
	}
}

func TestParseManifestRejectsUnknownFields(t *testing.T) {
	t.Parallel()
	value := testArtifact(t, "20260805210000_create_users")
	manifest, err := value.MarshalManifest()
	if err != nil {
		t.Fatal(err)
	}
	manifest = bytes.Replace(manifest, []byte("{\n"), []byte("{\n  \"unknown\": true,\n"), 1)
	if _, err := ParseManifest(manifest); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("expected unknown field error, got %v", err)
	}
}
