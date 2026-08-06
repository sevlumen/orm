package ormcli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/sevlumen/orm/migration"
	"github.com/sevlumen/orm/migration/artifact"
)

const maximumInputBytes = 16 << 20

func parseRisk(value string) (migration.Risk, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "safe":
		return migration.RiskSafe, nil
	case "review":
		return migration.RiskReview, nil
	case "destructive":
		return migration.RiskDestructive, nil
	default:
		return migration.RiskSafe, fmt.Errorf("risk must be safe, review, or destructive")
	}
}

func loadSnapshot(path string) (migration.Snapshot, error) {
	data, err := readLimited(path, maximumInputBytes)
	if err != nil {
		return migration.Snapshot{}, err
	}
	snapshot, err := migration.ParseSnapshot(data)
	if err != nil {
		return migration.Snapshot{}, fmt.Errorf("parse snapshot %s: %w", path, err)
	}
	return snapshot, nil
}

func readLimited(path string, maximum int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	if info.Size() > maximum {
		return nil, fmt.Errorf("%s exceeds %d bytes", path, maximum)
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat opened %s: %w", path, err)
	}
	if !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
		return nil, fmt.Errorf("%s changed while opening", path)
	}
	data, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if int64(len(data)) > maximum {
		return nil, fmt.Errorf("%s exceeds %d bytes", path, maximum)
	}
	return data, nil
}

type renameFile struct {
	Version int                `json:"version"`
	Renames []migration.Rename `json:"renames"`
}

func loadRenameOptions(path string) (migration.DiffOptions, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return migration.DiffOptions{}, nil
	}
	data, err := readLimited(path, maximumConfigBytes)
	if err != nil {
		return migration.DiffOptions{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var file renameFile
	if err := decoder.Decode(&file); err != nil {
		return migration.DiffOptions{}, fmt.Errorf("decode rename intents: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return migration.DiffOptions{}, fmt.Errorf("rename intent file contains multiple JSON values")
		}
		return migration.DiffOptions{}, fmt.Errorf("decode trailing rename data: %w", err)
	}
	if file.Version != 1 {
		return migration.DiffOptions{}, fmt.Errorf("unsupported rename intent version %d", file.Version)
	}
	for index, rename := range file.Renames {
		if err := rename.Validate(); err != nil {
			return migration.DiffOptions{}, fmt.Errorf("rename intent %d: %w", index, err)
		}
	}
	return migration.DiffOptions{Renames: append([]migration.Rename(nil), file.Renames...)}, nil
}

func localSnapshots(directory string) ([]string, []migration.Snapshot, error) {
	ids, err := artifact.List(directory)
	if err != nil {
		return nil, nil, err
	}
	snapshots := make([]migration.Snapshot, len(ids))
	for index, id := range ids {
		loaded, err := artifact.Load(directory, id)
		if err != nil {
			return nil, nil, fmt.Errorf("load migration %s: %w", id, err)
		}
		snapshot, err := migration.ParseSnapshot(loaded.SnapshotJSON)
		if err != nil {
			return nil, nil, fmt.Errorf("parse migration %s snapshot: %w", id, err)
		}
		snapshots[index] = snapshot
	}
	return ids, snapshots, nil
}

func latestLocalSnapshot(directory string) (migration.Snapshot, string, error) {
	ids, snapshots, err := localSnapshots(directory)
	if err != nil {
		return migration.Snapshot{}, "", err
	}
	if len(ids) == 0 {
		return migration.EmptySnapshot(), "", nil
	}
	return snapshots[len(snapshots)-1], ids[len(ids)-1], nil
}
