package releasepack

import "testing"

func TestConfigNormalization(t *testing.T) {
	config := Config{
		Root:    ".",
		Output:  "dist",
		Version: "v1.0.0-rc.1",
		Commit:  "0123456789abcdef",
		Date:    "2026-08-06T04:00:00+00:00",
		Targets: []Target{{GOOS: "linux", GOARCH: "amd64"}},
	}
	if _, err := config.normalize(); err != nil {
		t.Fatal(err)
	}
	if config.Date != "2026-08-06T04:00:00Z" {
		t.Fatalf("date=%q", config.Date)
	}
}

func TestConfigRejectsUnsafeReleaseInputs(t *testing.T) {
	base := Config{
		Root:    ".",
		Output:  "dist",
		Version: "v1.0.0",
		Commit:  "0123456789abcdef",
		Date:    "2026-08-06T04:00:00Z",
		Targets: []Target{{GOOS: "linux", GOARCH: "amd64"}},
	}
	for _, test := range []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "dirty", mutate: func(value *Config) { value.Dirty = true }},
		{name: "version", mutate: func(value *Config) { value.Version = "1.0.0" }},
		{name: "commit", mutate: func(value *Config) { value.Commit = "short" }},
		{name: "date", mutate: func(value *Config) { value.Date = "today" }},
		{name: "target", mutate: func(value *Config) { value.Targets = []Target{{GOOS: "plan9", GOARCH: "amd64"}} }},
		{name: "duplicate", mutate: func(value *Config) { value.Targets = append(value.Targets, value.Targets[0]) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := base
			config.Targets = append([]Target(nil), base.Targets...)
			test.mutate(&config)
			if _, err := config.normalize(); err == nil {
				t.Fatalf("accepted unsafe config: %#v", config)
			}
		})
	}
}
