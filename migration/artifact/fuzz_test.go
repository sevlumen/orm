package artifact

import (
	"encoding/json"
	"reflect"
	"testing"
)

func FuzzParseManifest(f *testing.F) {
	valid := Manifest{
		FormatVersion: FormatVersion,
		ID:            "20260806000000_init",
		Risk:          "safe",
		Files: FileChecksums{
			UpSQL:        zeroDigest(),
			DownSQL:      zeroDigest(),
			SnapshotJSON: zeroDigest(),
		},
	}
	validJSON, err := json.Marshal(valid)
	if err != nil {
		f.Fatal(err)
	}
	for _, seed := range [][]byte{
		validJSON,
		{},
		[]byte(`null`),
		[]byte(`{"formatVersion":1,"id":"20260806000000_init","risk":"safe","files":{}}`),
		[]byte(`{"formatVersion":1,"id":"../../escape","risk":"safe"}`),
		[]byte(`{"formatVersion":1,"id":"20260806000000_init","risk":"safe","unknown":"'; DROP TABLE users; --"}`),
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input []byte) {
		manifest, err := ParseManifest(input)
		if err != nil {
			return
		}
		canonical, err := json.Marshal(manifest)
		if err != nil {
			t.Fatalf("accepted manifest does not marshal: %v", err)
		}
		reparsed, err := ParseManifest(canonical)
		if err != nil {
			t.Fatalf("canonical manifest does not parse: %v", err)
		}
		if !reflect.DeepEqual(manifest, reparsed) {
			t.Fatalf("manifest round trip changed value: before=%#v after=%#v", manifest, reparsed)
		}
	})
}

func zeroDigest() string {
	return "0000000000000000000000000000000000000000000000000000000000000000"
}
