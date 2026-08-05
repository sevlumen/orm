// Package migration provides versioned schema snapshots and migration plans.
package migration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/sevlumen/orm/schema"
)

const SnapshotVersion = 1

// Snapshot is the versioned on-disk representation used for schema diffs.
type Snapshot struct {
	Version int           `json:"version"`
	Schema  schema.Schema `json:"schema"`
}

// EmptySnapshot returns the starting point for an application's first migration.
func EmptySnapshot() Snapshot {
	return Snapshot{Version: SnapshotVersion, Schema: schema.Schema{}}
}

// NewSnapshot validates and canonicalizes a schema for persistence.
func NewSnapshot(model schema.Schema) (Snapshot, error) {
	if err := model.Validate(); err != nil {
		return Snapshot{}, err
	}
	canonical := cloneSchema(model)
	sort.Slice(canonical.Tables, func(i, j int) bool {
		return canonical.Tables[i].Name < canonical.Tables[j].Name
	})
	return Snapshot{Version: SnapshotVersion, Schema: canonical}, nil
}

// Marshal returns deterministic, human-reviewable JSON.
func (s Snapshot) Marshal() ([]byte, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("migration: marshal snapshot: %w", err)
	}
	return append(data, '\n'), nil
}

// ParseSnapshot decodes a snapshot strictly and rejects unknown fields.
func ParseSnapshot(data []byte) (Snapshot, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var snapshot Snapshot
	if err := decoder.Decode(&snapshot); err != nil {
		return Snapshot{}, fmt.Errorf("migration: decode snapshot: %w", err)
	}
	if err := ensureEOF(decoder); err != nil {
		return Snapshot{}, err
	}
	if err := snapshot.Validate(); err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

// Validate checks the snapshot format version and schema invariants.
func (s Snapshot) Validate() error {
	if s.Version != SnapshotVersion {
		return fmt.Errorf("migration: unsupported snapshot version %d", s.Version)
	}
	if err := s.Schema.Validate(); err != nil {
		return fmt.Errorf("migration: invalid snapshot schema: %w", err)
	}
	return nil
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err == io.EOF {
		return nil
	} else if err != nil {
		return fmt.Errorf("migration: decode trailing data: %w", err)
	}
	return fmt.Errorf("migration: snapshot contains multiple JSON values")
}

func cloneSchema(model schema.Schema) schema.Schema {
	result := schema.Schema{Tables: make([]schema.Table, len(model.Tables))}
	for i, table := range model.Tables {
		result.Tables[i] = table
		result.Tables[i].Columns = append([]schema.Column(nil), table.Columns...)
	}
	return result
}
