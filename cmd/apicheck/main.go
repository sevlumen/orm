package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/sevlumen/orm/internal/apicheck"
)

func main() {
	var baseline string
	var root string
	var write bool
	flag.StringVar(&baseline, "baseline", "api/v1", "public API baseline file or directory")
	flag.StringVar(&root, "root", ".", "module root")
	flag.BoolVar(&write, "write", false, "replace the baseline with the current public API")
	flag.Parse()

	current, err := apicheck.Generate(root)
	if err != nil {
		fatal(err)
	}
	if write {
		if err := writeBaseline(baseline, current); err != nil {
			fatal(err)
		}
		fmt.Printf("wrote %s\n", baseline)
		return
	}

	expected, err := readBaseline(baseline)
	if err != nil {
		fatal(err)
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

func readBaseline(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("read baseline: %w", err)
	}
	if !info.IsDir() {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read baseline: %w", err)
		}
		return data, nil
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("read baseline directory: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	var result []byte
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".txt" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(path, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read baseline part %s: %w", entry.Name(), err)
		}
		result = append(result, data...)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("baseline directory %s contains no .txt parts", path)
	}
	return result, nil
}

func writeBaseline(path string, current []byte) error {
	info, err := os.Stat(path)
	if err == nil && info.IsDir() {
		entries, err := os.ReadDir(path)
		if err != nil {
			return fmt.Errorf("read baseline directory: %w", err)
		}
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".txt" {
				continue
			}
			if err := os.Remove(filepath.Join(path, entry.Name())); err != nil {
				return fmt.Errorf("remove baseline part %s: %w", entry.Name(), err)
			}
		}
		if err := os.WriteFile(filepath.Join(path, "surface.txt"), current, 0o644); err != nil {
			return fmt.Errorf("write baseline: %w", err)
		}
		return nil
	}
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("inspect baseline: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create baseline directory: %w", err)
	}
	if err := os.WriteFile(path, current, fs.FileMode(0o644)); err != nil {
		return fmt.Errorf("write baseline: %w", err)
	}
	return nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
