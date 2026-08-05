// Package artifact creates, validates, reads, and writes versioned migration artifacts.
package artifact

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/sevlumen/orm/migration"
	"github.com/sevlumen/orm/postgres"
)

const (
	// FormatVersion is the current on-disk migration artifact format.
	FormatVersion = 1

	ManifestFile = "manifest.json"
	UpFile       = "up.sql"
	DownFile     = "down.sql"
	SnapshotFile = "snapshot.json"
)

var migrationIDPattern = regexp.MustCompile(`^[0-9]{14}_[a-z][a-z0-9]*(?:_[a-z0-9]+)*$`)

// FileChecksums records SHA-256 digests for every artifact payload.
type FileChecksums struct {
	UpSQL        string `json:"up.sql"`
	DownSQL      string `json:"down.sql"`
	SnapshotJSON string `json:"snapshot.json"`
}

// Manifest is the canonical metadata for one migration directory.
type Manifest struct {
	FormatVersion int           `json:"formatVersion"`
	ID            string        `json:"id"`
	Risk          string        `json:"risk"`
	Warnings      []string      `json:"warnings,omitempty"`
	Files         FileChecksums `json:"files"`
}

// Artifact is a complete, verified migration directory held in memory.
type Artifact struct {
	Manifest     Manifest
	UpSQL        []byte
	DownSQL      []byte
	SnapshotJSON []byte
}

// Build creates a deterministic artifact from generated SQL and the next snapshot.
func Build(id string, generated postgres.MigrationSQL, next migration.Snapshot) (Artifact, error) {
	if err := ValidateID(id); err != nil {
		return Artifact{}, err
	}
	if generated.Risk > migration.RiskDestructive {
		return Artifact{}, fmt.Errorf("artifact: invalid migration risk %d", generated.Risk)
	}

	snapshotJSON, err := next.Marshal()
	if err != nil {
		return Artifact{}, fmt.Errorf("artifact: marshal snapshot: %w", err)
	}
	upSQL, err := normalizeSQL(generated.Up, UpFile)
	if err != nil {
		return Artifact{}, err
	}
	downSQL, err := normalizeSQL(generated.Down, DownFile)
	if err != nil {
		return Artifact{}, err
	}

	result := Artifact{
		UpSQL:        upSQL,
		DownSQL:      downSQL,
		SnapshotJSON: snapshotJSON,
	}
	result.Manifest = Manifest{
		FormatVersion: FormatVersion,
		ID:            id,
		Risk:          generated.Risk.String(),
		Warnings:      append([]string(nil), generated.Warnings...),
		Files: FileChecksums{
			UpSQL:        digest(upSQL),
			DownSQL:      digest(downSQL),
			SnapshotJSON: digest(snapshotJSON),
		},
	}
	if err := result.Validate(); err != nil {
		return Artifact{}, err
	}
	return result, nil
}

// ValidateID validates the portable, sortable migration ID format.
func ValidateID(id string) error {
	if !migrationIDPattern.MatchString(id) {
		return fmt.Errorf("artifact: migration ID %q must match YYYYMMDDHHMMSS_lower_snake_case", id)
	}
	return nil
}

// Validate verifies metadata, snapshot content, and payload checksums.
func (a Artifact) Validate() error {
	if err := a.Manifest.Validate(); err != nil {
		return err
	}
	if len(a.UpSQL) == 0 {
		return fmt.Errorf("artifact: %s is empty", UpFile)
	}
	if len(a.DownSQL) == 0 {
		return fmt.Errorf("artifact: %s is empty", DownFile)
	}
	if _, err := migration.ParseSnapshot(a.SnapshotJSON); err != nil {
		return fmt.Errorf("artifact: invalid %s: %w", SnapshotFile, err)
	}
	checks := []struct {
		name string
		want string
		data []byte
	}{
		{name: UpFile, want: a.Manifest.Files.UpSQL, data: a.UpSQL},
		{name: DownFile, want: a.Manifest.Files.DownSQL, data: a.DownSQL},
		{name: SnapshotFile, want: a.Manifest.Files.SnapshotJSON, data: a.SnapshotJSON},
	}
	for _, check := range checks {
		if got := digest(check.data); got != check.want {
			return fmt.Errorf("artifact: checksum mismatch for %s: got %s, want %s", check.name, got, check.want)
		}
	}
	return nil
}

// Validate checks manifest invariants without reading payload files.
func (m Manifest) Validate() error {
	if m.FormatVersion != FormatVersion {
		return fmt.Errorf("artifact: unsupported format version %d", m.FormatVersion)
	}
	if err := ValidateID(m.ID); err != nil {
		return err
	}
	switch m.Risk {
	case migration.RiskSafe.String(), migration.RiskReview.String(), migration.RiskDestructive.String():
	default:
		return fmt.Errorf("artifact: invalid risk %q", m.Risk)
	}
	checksums := []struct {
		name  string
		value string
	}{
		{name: UpFile, value: m.Files.UpSQL},
		{name: DownFile, value: m.Files.DownSQL},
		{name: SnapshotFile, value: m.Files.SnapshotJSON},
	}
	for _, checksum := range checksums {
		decoded, err := hex.DecodeString(checksum.value)
		if err != nil || len(decoded) != sha256.Size {
			return fmt.Errorf("artifact: invalid SHA-256 checksum for %s", checksum.name)
		}
	}
	return nil
}

// MarshalManifest returns deterministic, human-reviewable manifest JSON.
func (a Artifact) MarshalManifest() ([]byte, error) {
	if err := a.Validate(); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(a.Manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("artifact: marshal manifest: %w", err)
	}
	return append(data, '\n'), nil
}

// ParseManifest strictly parses a manifest and rejects trailing or unknown data.
func ParseManifest(data []byte) (Manifest, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("artifact: decode manifest: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return Manifest{}, fmt.Errorf("artifact: manifest contains multiple JSON values")
		}
		return Manifest{}, fmt.Errorf("artifact: decode trailing manifest data: %w", err)
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func normalizeSQL(value, name string) ([]byte, error) {
	if strings.TrimSpace(value) == "" {
		return nil, fmt.Errorf("artifact: %s is empty", name)
	}
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	value = strings.TrimRight(value, "\n") + "\n"
	return []byte(value), nil
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
