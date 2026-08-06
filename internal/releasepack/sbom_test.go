package releasepack

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"path/filepath"
	"testing"
)

func TestGoSumSHA256ConvertsBase64DigestToHex(t *testing.T) {
	digest := make([]byte, 32)
	for index := range digest {
		digest[index] = byte(index)
	}
	value := "h1:" + base64.StdEncoding.EncodeToString(digest)
	got, ok := goSumSHA256(value)
	if !ok {
		t.Fatal("valid Go sum was rejected")
	}
	if want := hex.EncodeToString(digest); got != want {
		t.Fatalf("checksum=%q want=%q", got, want)
	}
	for _, invalid := range []string{"", "sha256:abc", "h1:not-base64", "h1:" + base64.StdEncoding.EncodeToString([]byte("short"))} {
		if _, ok := goSumSHA256(invalid); ok {
			t.Fatalf("accepted invalid Go sum %q", invalid)
		}
	}
}

func TestListModulesIncludesMainModuleFirst(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	modules, err := listModules(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(modules) == 0 || !modules[0].Main || modules[0].Path != modulePath {
		t.Fatalf("module graph starts with %#v", modules)
	}
}
