package doccheck

import (
	"bufio"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var markdownLink = regexp.MustCompile(`!?(?:\[[^\]]*\])\(([^)]+)\)`)

// Check validates local Markdown links under root. Absolute HTTP(S), mailto,
// and same-document fragment links are intentionally not fetched.
func Check(root string) error {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("doccheck: resolve root: %w", err)
	}
	var markdownFiles []string
	if err := filepath.WalkDir(absolute, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != absolute && skipDirectory(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			markdownFiles = append(markdownFiles, path)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("doccheck: discover Markdown: %w", err)
	}
	sort.Strings(markdownFiles)

	var failures []string
	for _, path := range markdownFiles {
		fileFailures, err := checkFile(absolute, path)
		if err != nil {
			return err
		}
		failures = append(failures, fileFailures...)
	}
	if len(failures) != 0 {
		sort.Strings(failures)
		return fmt.Errorf("doccheck: broken local links:\n%s", strings.Join(failures, "\n"))
	}
	return nil
}

func checkFile(root, path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("doccheck: open %s: %w", path, err)
	}
	defer file.Close()

	relative, err := filepath.Rel(root, path)
	if err != nil {
		return nil, err
	}
	var failures []string
	scanner := bufio.NewScanner(file)
	lineNumber := 0
	inFence := false
	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		for _, match := range markdownLink.FindAllStringSubmatch(line, -1) {
			target := strings.TrimSpace(match[1])
			if index := strings.IndexAny(target, " \t"); index >= 0 {
				target = target[:index]
			}
			decoded, err := url.PathUnescape(target)
			if err != nil {
				failures = append(failures, fmt.Sprintf("%s:%d invalid escaped link %q", filepath.ToSlash(relative), lineNumber, target))
				continue
			}
			if skipTarget(decoded) {
				continue
			}
			pathPart := decoded
			if index := strings.IndexByte(pathPart, '#'); index >= 0 {
				pathPart = pathPart[:index]
			}
			if pathPart == "" {
				continue
			}
			resolved := filepath.Clean(filepath.Join(filepath.Dir(path), filepath.FromSlash(pathPart)))
			if !withinRoot(root, resolved) {
				failures = append(failures, fmt.Sprintf("%s:%d link escapes repository: %q", filepath.ToSlash(relative), lineNumber, target))
				continue
			}
			if _, err := os.Stat(resolved); err != nil {
				failures = append(failures, fmt.Sprintf("%s:%d missing %q", filepath.ToSlash(relative), lineNumber, target))
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("doccheck: scan %s: %w", path, err)
	}
	return failures, nil
}

func skipTarget(target string) bool {
	lower := strings.ToLower(target)
	return target == "" || strings.HasPrefix(target, "#") ||
		strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "mailto:") || strings.HasPrefix(lower, "tel:") ||
		strings.HasPrefix(lower, "data:")
}

func skipDirectory(name string) bool {
	return name == ".git" || name == "vendor" || strings.HasPrefix(name, ".")
}

func withinRoot(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
