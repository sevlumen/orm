package buildinfo

import "testing"

func TestCurrentNormalizesInjectedMetadata(t *testing.T) {
	originalVersion, originalCommit, originalDate, originalDirty := Version, Commit, Date, Dirty
	t.Cleanup(func() {
		Version, Commit, Date, Dirty = originalVersion, originalCommit, originalDate, originalDirty
	})
	Version = "v1.0.0-rc.1"
	Commit = "0123456789abcdef"
	Date = "2026-08-06T00:00:00Z"
	Dirty = "false"

	info := Current()
	if info.Version != Version || info.Commit != Commit || info.Date != Date || info.Dirty {
		t.Fatalf("info=%#v", info)
	}
	if info.GoVersion == "" || info.GOOS == "" || info.GOARCH == "" {
		t.Fatalf("runtime metadata is incomplete: %#v", info)
	}
}

func TestCurrentFailsDirtyClosed(t *testing.T) {
	originalDirty := Dirty
	t.Cleanup(func() { Dirty = originalDirty })
	for _, value := range []string{"", "unknown", "not-a-bool", "true"} {
		Dirty = value
		if !Current().Dirty {
			t.Fatalf("Dirty=%q was treated as clean", value)
		}
	}
}
