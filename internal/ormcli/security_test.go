package ormcli

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestConfigAndSnapshotRejectSymbolicLinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic-link creation is not reliably available on Windows CI")
	}
	root := t.TempDir()
	configTarget := filepath.Join(root, "config-target.json")
	configLink := filepath.Join(root, "config-link.json")
	config := `{"version":1}`
	if err := os.WriteFile(configTarget, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(configTarget, configLink); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(configLink); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("loadConfig symlink error = %v", err)
	}

	snapshotTarget := filepath.Join(root, "snapshot-target.json")
	snapshotLink := filepath.Join(root, "snapshot-link.json")
	if err := os.WriteFile(snapshotTarget, []byte(`{"formatVersion":1,"schema":{"tables":[],"enums":[]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(snapshotTarget, snapshotLink); err != nil {
		t.Fatal(err)
	}
	if _, err := loadSnapshot(snapshotLink); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("loadSnapshot symlink error = %v", err)
	}
}

func TestProtectedErrorRedactsInjectionLikeCredentials(t *testing.T) {
	const databaseURL = "postgres://app:p%27%3BDROP%20TABLE%20users%3B--@example.invalid/database"
	secrets := databaseSecrets(databaseURL)
	message := databaseURL + " decoded=p';DROP TABLE users;-- encoded=p%27%3BDROP%20TABLE%20users%3B--"
	protected := protectError(errors.New(message), secrets).Error()
	for _, forbidden := range []string{
		databaseURL,
		"p';DROP TABLE users;--",
		"p%27%3BDROP%20TABLE%20users%3B--",
	} {
		if strings.Contains(protected, forbidden) {
			t.Fatalf("protected error leaked %q: %s", forbidden, protected)
		}
	}
	if !strings.Contains(protected, "[REDACTED]") {
		t.Fatalf("protected error lacks redaction marker: %s", protected)
	}
}

func TestStrictSecurityInputsRejectUnknownAndTrailingJSON(t *testing.T) {
	root := t.TempDir()
	for _, test := range []struct {
		name    string
		content string
	}{
		{name: "unknown", content: `{"version":1,"unexpected":"'; DROP TABLE users; --"}`},
		{name: "trailing", content: `{"version":1} {"version":1}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(root, test.name+".json")
			if err := os.WriteFile(path, []byte(test.content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadConfig(path); err == nil {
				t.Fatalf("loadConfig accepted %s JSON", test.name)
			}
		})
	}
}
