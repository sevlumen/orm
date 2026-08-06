// Command releasepack builds deterministic Sevlumen ORM release artifacts.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/sevlumen/orm/internal/releasepack"
)

type targetList []releasepack.Target

func (values *targetList) String() string {
	parts := make([]string, len(*values))
	for index, value := range *values {
		parts[index] = value.GOOS + "/" + value.GOARCH
	}
	return strings.Join(parts, ",")
}

func (values *targetList) Set(input string) error {
	parts := strings.Split(strings.TrimSpace(input), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("target must use goos/goarch")
	}
	*values = append(*values, releasepack.Target{GOOS: parts[0], GOARCH: parts[1]})
	return nil
}

func main() {
	var targets targetList
	root := flag.String("root", ".", "repository root")
	output := flag.String("out", "dist", "empty output directory")
	version := flag.String("version", "", "v-prefixed semantic version")
	commit := flag.String("commit", "", "release commit SHA")
	date := flag.String("date", "", "release commit time in RFC3339")
	dirty := flag.Bool("dirty", false, "mark source tree dirty; release builds reject this")
	flag.Var(&targets, "target", "release target goos/goarch; repeat; defaults to all supported targets")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "releasepack: unexpected positional arguments")
		os.Exit(2)
	}
	config := releasepack.Config{
		Root:    *root,
		Output:  *output,
		Version: *version,
		Commit:  *commit,
		Date:    *date,
		Dirty:   *dirty,
	}
	if targets != nil {
		config.Targets = append([]releasepack.Target(nil), targets...)
	}
	manifest, err := releasepack.Build(context.Background(), config)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("built %d targets for %s at %s\n", len(manifest.Targets), manifest.Version, *output)
}
