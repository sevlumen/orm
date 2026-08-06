package buildinfo

import (
	"runtime"
	"strconv"
	"strings"
)

// Values are replaced through -ldflags -X by the release build.
var (
	Version = "devel"
	Commit  = "unknown"
	Date    = "unknown"
	Dirty   = "true"
)

// Info is the stable version payload reported by shipped commands.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	Date      string `json:"date"`
	Dirty     bool   `json:"dirty"`
	GoVersion string `json:"goVersion"`
	GOOS      string `json:"goos"`
	GOARCH    string `json:"goarch"`
}

// Current returns normalized compile-time and runtime metadata.
func Current() Info {
	return Info{
		Version:   fallback(strings.TrimSpace(Version), "devel"),
		Commit:    fallback(strings.TrimSpace(Commit), "unknown"),
		Date:      fallback(strings.TrimSpace(Date), "unknown"),
		Dirty:     parseDirty(Dirty),
		GoVersion: runtime.Version(),
		GOOS:      runtime.GOOS,
		GOARCH:    runtime.GOARCH,
	}
}

func parseDirty(value string) bool {
	parsed, err := strconv.ParseBool(strings.TrimSpace(value))
	return err != nil || parsed
}

func fallback(value, fallbackValue string) string {
	if value == "" {
		return fallbackValue
	}
	return value
}
