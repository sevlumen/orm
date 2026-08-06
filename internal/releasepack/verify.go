package releasepack

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Verify checks release checksums, manifest consistency, file set, and archive
// entry boundaries without executing any released binary.
func Verify(directory string) (Manifest, error) {
	root, err := filepath.Abs(directory)
	if err != nil {
		return Manifest{}, fmt.Errorf("releasepack: resolve verification directory: %w", err)
	}
	checksums, err := readChecksums(filepath.Join(root, "SHA256SUMS"))
	if err != nil {
		return Manifest{}, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return Manifest{}, fmt.Errorf("releasepack: read verification directory: %w", err)
	}
	actualNames := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return Manifest{}, fmt.Errorf("releasepack: unexpected non-regular release entry %q", entry.Name())
		}
		actualNames = append(actualNames, entry.Name())
	}
	expectedNames := make([]string, 0, len(checksums)+1)
	for name := range checksums {
		expectedNames = append(expectedNames, name)
	}
	expectedNames = append(expectedNames, "SHA256SUMS")
	sort.Strings(actualNames)
	sort.Strings(expectedNames)
	if strings.Join(actualNames, "\n") != strings.Join(expectedNames, "\n") {
		return Manifest{}, fmt.Errorf("releasepack: release file set differs: actual=%v expected=%v", actualNames, expectedNames)
	}
	for name, expected := range checksums {
		actual, err := hashFile(filepath.Join(root, name))
		if err != nil {
			return Manifest{}, err
		}
		if actual != expected {
			return Manifest{}, fmt.Errorf("releasepack: checksum mismatch for %s: got %s want %s", name, actual, expected)
		}
	}

	manifestData, err := os.ReadFile(filepath.Join(root, "release-manifest.json"))
	if err != nil {
		return Manifest{}, fmt.Errorf("releasepack: read release manifest: %w", err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(manifestData)))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("releasepack: decode release manifest: %w", err)
	}
	if manifest.Dirty || !releaseVersionPattern.MatchString(manifest.Version) || manifest.Commit == "" || manifest.Date == "" {
		return Manifest{}, fmt.Errorf("releasepack: invalid release manifest metadata")
	}
	for _, file := range manifest.Files {
		if checksums[file.Name] != file.SHA256 {
			return Manifest{}, fmt.Errorf("releasepack: manifest checksum differs for %s", file.Name)
		}
		info, err := os.Stat(filepath.Join(root, file.Name))
		if err != nil {
			return Manifest{}, err
		}
		if info.Size() != file.Size {
			return Manifest{}, fmt.Errorf("releasepack: manifest size differs for %s", file.Name)
		}
	}
	for _, target := range manifest.Targets {
		base := fmt.Sprintf("sevlumen-orm_%s_%s_%s", versionWithoutPrefix(manifest.Version), target.GOOS, target.GOARCH)
		name := base + ".tar.gz"
		if target.GOOS == "windows" {
			name = base + ".zip"
		}
		if _, exists := checksums[name]; !exists {
			return Manifest{}, fmt.Errorf("releasepack: missing archive %s", name)
		}
		if err := verifyBinaryArchive(filepath.Join(root, name), base, target.GOOS == "windows"); err != nil {
			return Manifest{}, err
		}
	}
	sourceName := fmt.Sprintf("sevlumen-orm_%s_source.tar.gz", versionWithoutPrefix(manifest.Version))
	if _, exists := checksums[sourceName]; !exists {
		return Manifest{}, fmt.Errorf("releasepack: missing source archive")
	}
	if err := verifyTarPaths(filepath.Join(root, sourceName), nil); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func readChecksums(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("releasepack: open SHA256SUMS: %w", err)
	}
	defer file.Close()
	result := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "  ", 2)
		if len(parts) != 2 || len(parts[0]) != sha256.Size*2 {
			return nil, fmt.Errorf("releasepack: invalid checksum line %q", line)
		}
		if _, err := hex.DecodeString(parts[0]); err != nil {
			return nil, fmt.Errorf("releasepack: invalid checksum digest for %q", parts[1])
		}
		name := parts[1]
		if filepath.Base(name) != name || name == "SHA256SUMS" || name == "" {
			return nil, fmt.Errorf("releasepack: invalid checksum filename %q", name)
		}
		if _, exists := result[name]; exists {
			return nil, fmt.Errorf("releasepack: duplicate checksum for %q", name)
		}
		result[name] = parts[0]
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("releasepack: empty SHA256SUMS")
	}
	return result, nil
}

func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func verifyBinaryArchive(path, root string, windows bool) error {
	extension := ""
	if windows {
		extension = ".exe"
	}
	expected := map[string]struct{}{
		root + "/LICENSE":          {},
		root + "/README.md":        {},
		root + "/orm" + extension:    {},
		root + "/ormgen" + extension: {},
	}
	if windows {
		reader, err := zip.OpenReader(path)
		if err != nil {
			return fmt.Errorf("releasepack: open zip %s: %w", filepath.Base(path), err)
		}
		defer reader.Close()
		actual := make(map[string]struct{}, len(reader.File))
		for _, file := range reader.File {
			if err := validateArchivePath(file.Name); err != nil {
				return err
			}
			if file.Mode()&os.ModeSymlink != 0 || !file.Mode().IsRegular() {
				return fmt.Errorf("releasepack: non-regular zip entry %q", file.Name)
			}
			actual[file.Name] = struct{}{}
		}
		return compareArchiveEntries(actual, expected, path)
	}
	return verifyTarPaths(path, expected)
}

func verifyTarPaths(path string, expected map[string]struct{}) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("releasepack: open tar gzip %s: %w", filepath.Base(path), err)
	}
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)
	actual := make(map[string]struct{})
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if err := validateArchivePath(header.Name); err != nil {
			return err
		}
		if header.Typeflag == tar.TypeDir {
			continue
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return fmt.Errorf("releasepack: non-regular tar entry %q", header.Name)
		}
		actual[header.Name] = struct{}{}
	}
	if expected != nil {
		return compareArchiveEntries(actual, expected, path)
	}
	if len(actual) == 0 {
		return fmt.Errorf("releasepack: source archive is empty")
	}
	return nil
}

func validateArchivePath(name string) error {
	if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, "\\") {
		return fmt.Errorf("releasepack: unsafe archive path %q", name)
	}
	cleaned := filepath.ToSlash(filepath.Clean(name))
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") || cleaned != strings.TrimSuffix(name, "/") {
		return fmt.Errorf("releasepack: unsafe archive path %q", name)
	}
	return nil
}

func compareArchiveEntries(actual, expected map[string]struct{}, path string) error {
	if len(actual) != len(expected) {
		return fmt.Errorf("releasepack: unexpected entries in %s: actual=%v expected=%v", filepath.Base(path), mapKeys(actual), mapKeys(expected))
	}
	for name := range expected {
		if _, exists := actual[name]; !exists {
			return fmt.Errorf("releasepack: missing %q in %s", name, filepath.Base(path))
		}
	}
	return nil
}

func mapKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
