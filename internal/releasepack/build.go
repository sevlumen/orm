package releasepack

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const modulePath = "github.com/sevlumen/orm"

// Manifest describes the immutable inputs and generated release files.
type Manifest struct {
	Version string         `json:"version"`
	Commit  string         `json:"commit"`
	Date    string         `json:"date"`
	Dirty   bool           `json:"dirty"`
	Targets []Target       `json:"targets"`
	Files   []ManifestFile `json:"files"`
}

// ManifestFile records one release payload.
type ManifestFile struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// Build creates deterministic binary archives, a source archive, SPDX SBOM,
// manifest, and SHA256SUMS in an empty output directory.
func Build(ctx context.Context, config Config) (Manifest, error) {
	buildTime, err := config.normalize()
	if err != nil {
		return Manifest{}, err
	}
	root, err := filepath.Abs(config.Root)
	if err != nil {
		return Manifest{}, fmt.Errorf("releasepack: resolve root: %w", err)
	}
	output, err := filepath.Abs(config.Output)
	if err != nil {
		return Manifest{}, fmt.Errorf("releasepack: resolve output: %w", err)
	}
	if root == output || strings.HasPrefix(root, output+string(filepath.Separator)) {
		return Manifest{}, fmt.Errorf("releasepack: output cannot contain the source root")
	}
	if err := prepareOutput(output); err != nil {
		return Manifest{}, err
	}
	staging, err := os.MkdirTemp("", "sevlumen-release-")
	if err != nil {
		return Manifest{}, fmt.Errorf("releasepack: create staging directory: %w", err)
	}
	defer os.RemoveAll(staging)

	var generated []string
	for _, target := range config.Targets {
		name, err := buildTarget(ctx, root, output, staging, config, target, buildTime)
		if err != nil {
			return Manifest{}, err
		}
		generated = append(generated, name)
	}
	sourceName, err := buildSourceArchive(ctx, root, output, config, buildTime)
	if err != nil {
		return Manifest{}, err
	}
	generated = append(generated, sourceName)

	sbomName := fmt.Sprintf("sevlumen-orm_%s_sbom.spdx.json", versionWithoutPrefix(config.Version))
	if err := writeSBOM(ctx, root, filepath.Join(output, sbomName), config, buildTime); err != nil {
		return Manifest{}, err
	}
	generated = append(generated, sbomName)

	manifest := Manifest{
		Version: config.Version,
		Commit:  config.Commit,
		Date:    config.Date,
		Dirty:   config.Dirty,
		Targets: append([]Target(nil), config.Targets...),
	}
	for _, name := range generated {
		file, err := inspectFile(filepath.Join(output, name))
		if err != nil {
			return Manifest{}, err
		}
		manifest.Files = append(manifest.Files, file)
	}
	sort.Slice(manifest.Files, func(i, j int) bool { return manifest.Files[i].Name < manifest.Files[j].Name })
	manifestName := "release-manifest.json"
	if err := writeJSONExclusive(filepath.Join(output, manifestName), manifest); err != nil {
		return Manifest{}, err
	}
	generated = append(generated, manifestName)
	if err := writeChecksums(output, generated); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func prepareOutput(output string) error {
	info, err := os.Stat(output)
	if err != nil {
		if os.IsNotExist(err) {
			if err := os.MkdirAll(output, 0o755); err != nil {
				return fmt.Errorf("releasepack: create output directory: %w", err)
			}
			return nil
		}
		return fmt.Errorf("releasepack: inspect output directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("releasepack: output %s is not a directory", output)
	}
	entries, err := os.ReadDir(output)
	if err != nil {
		return fmt.Errorf("releasepack: read output directory: %w", err)
	}
	if len(entries) != 0 {
		return fmt.Errorf("releasepack: output directory must be empty")
	}
	return nil
}

func buildTarget(ctx context.Context, root, output, staging string, config Config, target Target, buildTime time.Time) (string, error) {
	base := fmt.Sprintf("sevlumen-orm_%s_%s_%s", versionWithoutPrefix(config.Version), target.GOOS, target.GOARCH)
	targetDirectory := filepath.Join(staging, target.GOOS+"_"+target.GOARCH)
	if err := os.MkdirAll(targetDirectory, 0o755); err != nil {
		return "", fmt.Errorf("releasepack: create target staging: %w", err)
	}
	extension := ""
	if target.GOOS == "windows" {
		extension = ".exe"
	}
	ormPath := filepath.Join(targetDirectory, "orm"+extension)
	ormgenPath := filepath.Join(targetDirectory, "ormgen"+extension)
	for _, command := range []struct {
		packagePath string
		outputPath  string
	}{
		{packagePath: "./cmd/orm", outputPath: ormPath},
		{packagePath: "./cmd/ormgen", outputPath: ormgenPath},
	} {
		if err := buildCommand(ctx, root, config, target, command.packagePath, command.outputPath); err != nil {
			return "", err
		}
	}
	entries := []archiveEntry{
		{Name: "LICENSE", Path: filepath.Join(root, "LICENSE"), Mode: 0o644},
		{Name: "README.md", Path: filepath.Join(root, "README.md"), Mode: 0o644},
		{Name: "orm" + extension, Path: ormPath, Mode: 0o755},
		{Name: "ormgen" + extension, Path: ormgenPath, Mode: 0o755},
	}
	if target.GOOS == "windows" {
		name := base + ".zip"
		if err := writeZip(filepath.Join(output, name), base, entries, buildTime); err != nil {
			return "", fmt.Errorf("releasepack: write %s: %w", name, err)
		}
		return name, nil
	}
	name := base + ".tar.gz"
	if err := writeTarGzip(filepath.Join(output, name), base, entries, buildTime); err != nil {
		return "", fmt.Errorf("releasepack: write %s: %w", name, err)
	}
	return name, nil
}

func buildCommand(ctx context.Context, root string, config Config, target Target, packagePath, outputPath string) error {
	ldflags := strings.Join([]string{
		"-s",
		"-w",
		"-buildid=",
		"-X", modulePath + "/internal/buildinfo.Version=" + config.Version,
		"-X", modulePath + "/internal/buildinfo.Commit=" + config.Commit,
		"-X", modulePath + "/internal/buildinfo.Date=" + config.Date,
		"-X", modulePath + "/internal/buildinfo.Dirty=false",
	}, " ")
	command := exec.CommandContext(ctx, "go", "build", "-mod=readonly", "-trimpath", "-buildvcs=false", "-ldflags", ldflags, "-o", outputPath, packagePath)
	command.Dir = root
	command.Env = append(os.Environ(),
		"CGO_ENABLED=0",
		"GOOS="+target.GOOS,
		"GOARCH="+target.GOARCH,
		"SOURCE_DATE_EPOCH="+fmt.Sprintf("%d", mustUnix(config.Date)),
	)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("releasepack: build %s for %s/%s: %w: %s", packagePath, target.GOOS, target.GOARCH, err, strings.TrimSpace(string(output)))
	}
	if _, err := regularFileInfo(outputPath); err != nil {
		return err
	}
	return nil
}

func buildSourceArchive(ctx context.Context, root, output string, config Config, buildTime time.Time) (string, error) {
	command := exec.CommandContext(ctx, "git", "ls-files", "-z")
	command.Dir = root
	data, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("releasepack: list tracked source files: %w", err)
	}
	var entries []archiveEntry
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Split(splitNUL)
	for scanner.Scan() {
		name := scanner.Text()
		if name == "" {
			continue
		}
		path := filepath.Join(root, filepath.FromSlash(name))
		info, err := regularFileInfo(path)
		if err != nil {
			return "", err
		}
		mode := os.FileMode(0o644)
		if info.Mode()&0o111 != 0 {
			mode = 0o755
		}
		entries = append(entries, archiveEntry{Name: name, Path: path, Mode: mode})
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("releasepack: parse tracked files: %w", err)
	}
	name := fmt.Sprintf("sevlumen-orm_%s_source.tar.gz", versionWithoutPrefix(config.Version))
	rootName := fmt.Sprintf("sevlumen-orm_%s_source", versionWithoutPrefix(config.Version))
	if err := writeTarGzip(filepath.Join(output, name), rootName, entries, buildTime); err != nil {
		return "", fmt.Errorf("releasepack: write source archive: %w", err)
	}
	return name, nil
}

func splitNUL(data []byte, atEOF bool) (advance int, token []byte, err error) {
	for index, value := range data {
		if value == 0 {
			return index + 1, data[:index], nil
		}
	}
	if atEOF && len(data) != 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

func inspectFile(path string) (ManifestFile, error) {
	info, err := regularFileInfo(path)
	if err != nil {
		return ManifestFile{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return ManifestFile{}, err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return ManifestFile{}, err
	}
	return ManifestFile{Name: filepath.Base(path), SHA256: hex.EncodeToString(hash.Sum(nil)), Size: info.Size()}, nil
}

func writeChecksums(output string, names []string) (err error) {
	sort.Strings(names)
	path := filepath.Join(output, "SHA256SUMS")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := file.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()
	for _, name := range names {
		value, err := inspectFile(filepath.Join(output, name))
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(file, "%s  %s\n", value.SHA256, value.Name); err != nil {
			return err
		}
	}
	return file.Sync()
}

func writeJSONExclusive(path string, value any) (err error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := file.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return err
	}
	return file.Sync()
}

func mustUnix(date string) int64 {
	value, err := time.Parse(time.RFC3339, date)
	if err != nil {
		panic(err)
	}
	return value.Unix()
}
