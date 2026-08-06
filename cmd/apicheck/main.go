package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sevlumen/orm/internal/apicheck"
)

func main() {
	var baseline string
	var root string
	var write bool
	flag.StringVar(&baseline, "baseline", "api/v1.txt", "public API baseline file")
	flag.StringVar(&root, "root", ".", "module root")
	flag.BoolVar(&write, "write", false, "replace the baseline with the current public API")
	flag.Parse()

	current, err := apicheck.Generate(root)
	if err != nil {
		fatal(err)
	}
	if write {
		if err := os.MkdirAll(filepath.Dir(baseline), 0o755); err != nil {
			fatal(fmt.Errorf("create baseline directory: %w", err))
		}
		if err := os.WriteFile(baseline, current, 0o644); err != nil {
			fatal(fmt.Errorf("write baseline: %w", err))
		}
		fmt.Printf("wrote %s\n", baseline)
		return
	}

	expected, err := os.ReadFile(baseline)
	if err != nil {
		fatal(fmt.Errorf("read baseline: %w", err))
	}
	if bytes.Equal(expected, current) {
		fmt.Printf("public API matches %s\n", baseline)
		return
	}
	fmt.Fprintf(os.Stderr, "public API differs from %s\n", baseline)
	fmt.Fprintln(os.Stderr, "regenerate only after reviewing compatibility: go run ./cmd/apicheck -write -baseline", baseline)
	fmt.Fprintln(os.Stderr, "--- BEGIN CURRENT API ---")
	_, _ = os.Stderr.Write(current)
	fmt.Fprintln(os.Stderr, "--- END CURRENT API ---")
	os.Exit(1)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
