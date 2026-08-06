package entity

import (
	"reflect"
	"testing"
)

func FuzzParseTag(f *testing.F) {
	for _, seed := range []string{
		"",
		"primaryKey;notNull",
		"column:user_id;type:uuid;default:gen_random_uuid()",
		"generated:lower(email);readOnly",
		"nullable;notNull",
		"unknown:value",
		"column:'; DROP TABLE users; --",
		"type:text); DROP TABLE users; --",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		first, firstErr := parseTag(input)
		second, secondErr := parseTag(input)
		if (firstErr == nil) != (secondErr == nil) {
			t.Fatalf("parseTag is non-deterministic: first=%v second=%v", firstErr, secondErr)
		}
		if firstErr != nil {
			return
		}
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("parseTag result is non-deterministic: first=%#v second=%#v", first, second)
		}
		for key, value := range first {
			if key == "" || value == "" {
				t.Fatalf("parseTag returned empty key/value: %#v", first)
			}
		}
	})
}
