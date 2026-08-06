// Command ormgen generates typed Sevlumen ORM query metadata from Go structs.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/sevlumen/orm/generator"
	"github.com/sevlumen/orm/internal/buildinfo"
)

type typeList []string

func (values *typeList) String() string { return strings.Join(*values, ",") }

func (values *typeList) Set(value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("type list cannot be empty")
	}
	*values = append(*values, value)
	return nil
}

func main() {
	var types typeList
	dir := flag.String("dir", ".", "package directory containing entity structs")
	output := flag.String("output", "orm_gen.go", "generated Go file inside the package directory")
	check := flag.Bool("check", false, "verify generated output without writing")
	version := flag.Bool("version", false, "print version and build metadata")
	versionJSON := flag.Bool("version-json", false, "print version metadata as JSON")
	flag.Var(&types, "type", "exported entity type; repeat the flag or use comma-separated names")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "ormgen: unexpected positional arguments")
		os.Exit(2)
	}
	if *version || *versionJSON {
		info := buildinfo.Current()
		if *versionJSON {
			if err := json.NewEncoder(os.Stdout).Encode(info); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		}
		fmt.Printf("ormgen %s commit=%s date=%s dirty=%t go=%s %s/%s\n", info.Version, info.Commit, info.Date, info.Dirty, info.GoVersion, info.GOOS, info.GOARCH)
		return
	}
	if err := generator.Write(generator.Config{Dir: *dir, Types: types, Output: *output, Check: *check}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
