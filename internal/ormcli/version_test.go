package ormcli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/sevlumen/orm/internal/buildinfo"
)

func TestVersionCommandHumanAndJSON(t *testing.T) {
	info := buildinfo.Info{
		Version:   "v1.0.0-rc.1",
		Commit:    "0123456789abcdef",
		Date:      "2026-08-06T00:00:00Z",
		Dirty:     false,
		GoVersion: "go1.25.12",
		GOOS:      "linux",
		GOARCH:    "amd64",
	}
	for _, test := range []struct {
		name string
		args []string
		want []string
	}{
		{name: "human", args: []string{"version"}, want: []string{"orm v1.0.0-rc.1", "commit=0123456789abcdef", "dirty=false", "linux/amd64"}},
		{name: "json", args: []string{"version", "--json"}, want: []string{`"version":1`, `"command":"version"`, `"version":"v1.0.0-rc.1"`, `"dirty":false`}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			app := New()
			app.Out, app.Err = &stdout, &stderr
			app.BuildInfo = func() buildinfo.Info { return info }
			if exit := app.Run(context.Background(), test.args); exit != 0 {
				t.Fatalf("exit=%d stderr=%s", exit, stderr.String())
			}
			for _, expected := range test.want {
				if !strings.Contains(stdout.String(), expected) {
					t.Fatalf("stdout=%q missing %q", stdout.String(), expected)
				}
			}
		})
	}
}

func TestVersionCommandRejectsUnexpectedArguments(t *testing.T) {
	var stderr bytes.Buffer
	app := New()
	app.Err = &stderr
	if exit := app.Run(context.Background(), []string{"version", "unexpected"}); exit != 2 {
		t.Fatalf("exit=%d stderr=%s", exit, stderr.String())
	}
}
