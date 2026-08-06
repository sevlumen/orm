package doccheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckValidLocalLinksAndIgnoresCodeFences(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "guide.md"), []byte("# Guide\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	readme := "[Guide](docs/guide.md#section)\n[Web](https://example.com)\n```\n[Ignored](missing.md)\n```\n"
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte(readme), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Check(root); err != nil {
		t.Fatal(err)
	}
}

func TestCheckReportsMissingAndEscapingLinks(t *testing.T) {
	root := t.TempDir()
	readme := "[Missing](docs/missing.md)\n[Escape](../../secret.md)\n"
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte(readme), 0o600); err != nil {
		t.Fatal(err)
	}
	err := Check(root)
	if err == nil {
		t.Fatal("expected broken-link error")
	}
	message := err.Error()
	if !strings.Contains(message, "missing") || !strings.Contains(message, "escapes repository") {
		t.Fatalf("error=%v", err)
	}
}
