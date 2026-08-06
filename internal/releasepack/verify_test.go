package releasepack

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestVerifyAcceptsExpectedReleaseAndRejectsTampering(t *testing.T) {
	root := t.TempDir()
	input := t.TempDir()
	for name, content := range map[string]string{
		"orm":       "orm-binary",
		"ormgen":    "ormgen-binary",
		"LICENSE":   "license",
		"README.md": "readme",
		"source.go": "package source",
	} {
		if err := os.WriteFile(filepath.Join(input, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	timestamp := time.Date(2026, 8, 6, 4, 0, 0, 0, time.UTC)
	version := "v1.0.0-rc.1"
	base := "sevlumen-orm_1.0.0-rc.1_linux_amd64"
	archiveName := base + ".tar.gz"
	if err := writeTarGzip(filepath.Join(root, archiveName), base, []archiveEntry{
		{Name: "LICENSE", Path: filepath.Join(input, "LICENSE"), Mode: 0o644},
		{Name: "README.md", Path: filepath.Join(input, "README.md"), Mode: 0o644},
		{Name: "orm", Path: filepath.Join(input, "orm"), Mode: 0o755},
		{Name: "ormgen", Path: filepath.Join(input, "ormgen"), Mode: 0o755},
	}, timestamp); err != nil {
		t.Fatal(err)
	}
	sourceName := "sevlumen-orm_1.0.0-rc.1_source.tar.gz"
	if err := writeTarGzip(filepath.Join(root, sourceName), "sevlumen-orm_1.0.0-rc.1_source", []archiveEntry{
		{Name: "source.go", Path: filepath.Join(input, "source.go"), Mode: 0o644},
	}, timestamp); err != nil {
		t.Fatal(err)
	}
	sbomName := "sevlumen-orm_1.0.0-rc.1_sbom.spdx.json"
	if err := os.WriteFile(filepath.Join(root, sbomName), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	manifest := Manifest{
		Version: version,
		Commit:  "0123456789abcdef",
		Date:    timestamp.Format(time.RFC3339),
		Dirty:   false,
		Targets: []Target{{GOOS: "linux", GOARCH: "amd64"}},
	}
	for _, name := range []string{archiveName, sourceName, sbomName} {
		file, err := inspectFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		manifest.Files = append(manifest.Files, file)
	}
	if err := writeJSONExclusive(filepath.Join(root, "release-manifest.json"), manifest); err != nil {
		t.Fatal(err)
	}
	if err := writeChecksums(root, []string{archiveName, sourceName, sbomName, "release-manifest.json"}); err != nil {
		t.Fatal(err)
	}

	verified, err := Verify(root)
	if err != nil {
		t.Fatal(err)
	}
	if verified.Version != version || len(verified.Targets) != 1 {
		t.Fatalf("verified manifest=%#v", verified)
	}

	file, err := os.OpenFile(filepath.Join(root, archiveName), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("tampered")); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(root); err == nil {
		t.Fatal("tampered release was accepted")
	}
}

func TestValidateArchivePathRejectsTraversal(t *testing.T) {
	for _, value := range []string{"", "/absolute", "../escape", "root/../../escape", `root\\escape`} {
		if err := validateArchivePath(value); err == nil {
			t.Fatalf("accepted unsafe archive path %q", value)
		}
	}
	for _, value := range []string{"root/file", "root/nested/file"} {
		if err := validateArchivePath(value); err != nil {
			t.Fatalf("rejected safe archive path %q: %v", value, err)
		}
	}
}
