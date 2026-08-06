package ormcli

import (
	"reflect"
	"testing"
)

func FuzzParseConfig(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte(`{"version":1}`),
		[]byte(`{"version":1,"migrations":{"maximumRisk":"review","timeout":"30s"}}`),
		[]byte(`{"version":1,"unexpected":"'; DROP TABLE users; --"}`),
		[]byte(`{"version":1} {"version":1}`),
		[]byte(`null`),
		{},
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input []byte) {
		first, firstErr := parseConfig(input)
		second, secondErr := parseConfig(input)
		if (firstErr == nil) != (secondErr == nil) {
			t.Fatalf("config parse is non-deterministic: first=%v second=%v", firstErr, secondErr)
		}
		if firstErr != nil {
			return
		}
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("config parse result is non-deterministic: first=%#v second=%#v", first, second)
		}
		if err := validateConfig(first); err != nil {
			t.Fatalf("accepted config does not validate: %v", err)
		}
	})
}
