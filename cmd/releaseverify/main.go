// Command releaseverify validates release artifacts without executing them.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/sevlumen/orm/internal/releasepack"
)

func main() {
	directory := flag.String("dir", "dist", "release artifact directory")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "releaseverify: unexpected positional arguments")
		os.Exit(2)
	}
	manifest, err := releasepack.Verify(*directory)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("verified %d targets for %s commit=%s\n", len(manifest.Targets), manifest.Version, manifest.Commit)
}
