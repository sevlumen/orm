package releasepack

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

var releaseVersionPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?$`)

// Target identifies one released operating system and architecture.
type Target struct {
	GOOS   string
	GOARCH string
}

// Config controls deterministic release construction.
type Config struct {
	Root    string
	Output  string
	Version string
	Commit  string
	Date    string
	Dirty   bool
	Targets []Target
}

// DefaultTargets are the v1 binary release platforms.
func DefaultTargets() []Target {
	return []Target{
		{GOOS: "darwin", GOARCH: "amd64"},
		{GOOS: "darwin", GOARCH: "arm64"},
		{GOOS: "linux", GOARCH: "amd64"},
		{GOOS: "linux", GOARCH: "arm64"},
		{GOOS: "windows", GOARCH: "amd64"},
		{GOOS: "windows", GOARCH: "arm64"},
	}
}

func (config *Config) normalize() (time.Time, error) {
	config.Root = strings.TrimSpace(config.Root)
	config.Output = strings.TrimSpace(config.Output)
	config.Version = strings.TrimSpace(config.Version)
	config.Commit = strings.TrimSpace(config.Commit)
	config.Date = strings.TrimSpace(config.Date)
	if config.Root == "" {
		return time.Time{}, fmt.Errorf("releasepack: root is required")
	}
	if config.Output == "" {
		return time.Time{}, fmt.Errorf("releasepack: output directory is required")
	}
	if !releaseVersionPattern.MatchString(config.Version) {
		return time.Time{}, fmt.Errorf("releasepack: version %q is not a v-prefixed semantic version", config.Version)
	}
	if len(config.Commit) < 7 || strings.ContainsAny(config.Commit, " \t\r\n") {
		return time.Time{}, fmt.Errorf("releasepack: commit must be a non-whitespace revision of at least 7 characters")
	}
	buildTime, err := time.Parse(time.RFC3339, config.Date)
	if err != nil {
		return time.Time{}, fmt.Errorf("releasepack: date must be RFC3339: %w", err)
	}
	buildTime = buildTime.UTC().Truncate(time.Second)
	config.Date = buildTime.Format(time.RFC3339)
	if config.Targets == nil {
		config.Targets = DefaultTargets()
	}
	if len(config.Targets) == 0 {
		return time.Time{}, fmt.Errorf("releasepack: at least one target is required")
	}
	seen := make(map[string]struct{}, len(config.Targets))
	for _, target := range config.Targets {
		if err := target.validate(); err != nil {
			return time.Time{}, err
		}
		key := target.GOOS + "/" + target.GOARCH
		if _, exists := seen[key]; exists {
			return time.Time{}, fmt.Errorf("releasepack: duplicate target %s", key)
		}
		seen[key] = struct{}{}
	}
	sort.Slice(config.Targets, func(i, j int) bool {
		if config.Targets[i].GOOS != config.Targets[j].GOOS {
			return config.Targets[i].GOOS < config.Targets[j].GOOS
		}
		return config.Targets[i].GOARCH < config.Targets[j].GOARCH
	})
	if config.Dirty {
		return time.Time{}, fmt.Errorf("releasepack: dirty source state cannot produce release artifacts")
	}
	return buildTime, nil
}

func (target Target) validate() error {
	supported := false
	for _, candidate := range DefaultTargets() {
		if target == candidate {
			supported = true
			break
		}
	}
	if !supported {
		return fmt.Errorf("releasepack: unsupported target %s/%s", target.GOOS, target.GOARCH)
	}
	return nil
}

func versionWithoutPrefix(version string) string {
	return strings.TrimPrefix(version, "v")
}
