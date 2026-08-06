package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/sevlumen/orm/internal/doccheck"
)

func main() {
	root := flag.String("root", ".", "repository root")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "doccheck: unexpected positional arguments")
		os.Exit(2)
	}
	if err := doccheck.Check(*root); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("documentation links are valid")
}
