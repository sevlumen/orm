package artifact

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const (
	maxManifestSize = 1 << 20
	maxSQLSize      = 16 << 20
	maxSnapshotSize = 8 << 20
)

type filesystemWriter struct {
	writeFile func(string, []byte, fs.FileMode) error
}

// Write atomically publishes an artifact directory under root.
func Write(root string, artifact Artifact) (string, error) {
	writer := filesystemWriter{writeFile: writeFileSync}
	return writer.write(root, artifact)
}

func (w filesystemWriter) write(root string, artifact Artifact) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", fmt.Errorf("artifact: root directory is required")
	}
	if w.writeFile == nil {
		return "", fmt.Errorf("artifact: writer is not configured")
	}
	if err := artifact.Validate(); err != nil {
		return "", err
	}
	manifestJSON, err := artifact.MarshalManifest()
	if err != nil {
		return "", err
	}

	root, err = filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("artifact: resolve root: %w", err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", fmt.Errorf("artifact: create root: %w", err)
	}

	target := filepath.Join(root, artifact.Manifest.ID)
	if _, err := os.Lstat(target); err == nil {
		return "", fmt.Errorf("artifact: migration %q already exists", artifact.Manifest.ID)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("artifact: inspect migration target: %w", err)
	}

	temporary, err := os.MkdirTemp(root, "."+artifact.Manifest.ID+".tmp-")
	if err != nil {
		return "", fmt.Errorf("artifact: create temporary directory: %w", err)
	}
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(temporary)
		}
	}()

	files := []struct {
		name string
		data []byte
	}{
		{name: UpFile, data: artifact.UpSQL},
		{name: DownFile, data: artifact.DownSQL},
		{name: SnapshotFile, data: artifact.SnapshotJSON},
		{name: ManifestFile, data: manifestJSON},
	}
	for _, file := range files {
		if err := w.writeFile(filepath.Join(temporary, file.name), file.data, 0o644); err != nil {
			return "", fmt.Errorf("artifact: write %s: %w", file.name, err)
		}
	}
	if err := syncDirectory(temporary); err != nil {
		return "", fmt.Errorf("artifact: sync temporary directory: %w", err)
	}

	if _, err := os.Lstat(target); err == nil {
		return "", fmt.Errorf("artifact: migration %q already exists", artifact.Manifest.ID)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("artifact: inspect migration target: %w", err)
	}
	if err := os.Rename(temporary, target); err != nil {
		return "", fmt.Errorf("artifact: publish migration: %w", err)
	}
	published = true
	return target, nil
}

// Load reads and verifies one migration artifact from root.
func Load(root, id string) (Artifact, error) {
	if strings.TrimSpace(root) == "" {
		return Artifact{}, fmt.Errorf("artifact: root directory is required")
	}
	if err := ValidateID(id); err != nil {
		return Artifact{}, err
	}
	base := filepath.Join(root, id)
	manifestJSON, err := readRegularFile(filepath.Join(base, ManifestFile), maxManifestSize)
	if err != nil {
		return Artifact{}, err
	}
	manifest, err := ParseManifest(manifestJSON)
	if err != nil {
		return Artifact{}, err
	}
	if manifest.ID != id {
		return Artifact{}, fmt.Errorf("artifact: manifest ID %q does not match directory %q", manifest.ID, id)
	}

	upSQL, err := readRegularFile(filepath.Join(base, UpFile), maxSQLSize)
	if err != nil {
		return Artifact{}, err
	}
	downSQL, err := readRegularFile(filepath.Join(base, DownFile), maxSQLSize)
	if err != nil {
		return Artifact{}, err
	}
	snapshotJSON, err := readRegularFile(filepath.Join(base, SnapshotFile), maxSnapshotSize)
	if err != nil {
		return Artifact{}, err
	}
	result := Artifact{Manifest: manifest, UpSQL: upSQL, DownSQL: downSQL, SnapshotJSON: snapshotJSON}
	if err := result.Validate(); err != nil {
		return Artifact{}, err
	}
	return result, nil
}

// List returns sorted migration IDs and rejects unexpected non-hidden entries.
func List(root string) ([]string, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("artifact: root directory is required")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("artifact: read migration root: %w", err)
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if !entry.IsDir() {
			return nil, fmt.Errorf("artifact: unexpected file %q in migration root", entry.Name())
		}
		if err := ValidateID(entry.Name()); err != nil {
			return nil, err
		}
		ids = append(ids, entry.Name())
	}
	sort.Strings(ids)
	return ids, nil
}

func writeFileSync(path string, data []byte, mode fs.FileMode) (err error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := file.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()
	if _, err := file.Write(data); err != nil {
		return err
	}
	return file.Sync()
}

func readRegularFile(path string, maximum int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("artifact: inspect %s: %w", filepath.Base(path), err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("artifact: %s is not a regular file", filepath.Base(path))
	}
	if info.Size() > maximum {
		return nil, fmt.Errorf("artifact: %s exceeds %d bytes", filepath.Base(path), maximum)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("artifact: open %s: %w", filepath.Base(path), err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil {
		return nil, fmt.Errorf("artifact: read %s: %w", filepath.Base(path), err)
	}
	if int64(len(data)) > maximum {
		return nil, fmt.Errorf("artifact: %s exceeds %d bytes", filepath.Base(path), maximum)
	}
	return data, nil
}

func syncDirectory(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
