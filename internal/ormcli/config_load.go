package ormcli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

func loadConfig(path string) (Config, error) {
	config := defaultConfig()
	path = strings.TrimSpace(path)
	if path == "" {
		return config, nil
	}
	data, err := readLimited(path, maximumConfigBytes)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var parsed Config
	if err := decoder.Decode(&parsed); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return Config{}, fmt.Errorf("config contains multiple JSON values")
		}
		return Config{}, fmt.Errorf("decode trailing config data: %w", err)
	}
	if parsed.Version != configVersion {
		return Config{}, fmt.Errorf("unsupported config version %d", parsed.Version)
	}
	mergeConfig(&config, parsed)
	if err := validateConfig(config); err != nil {
		return Config{}, err
	}
	return config, nil
}

func mergeConfig(destination *Config, source Config) {
	if source.Generate.Directory != "" {
		destination.Generate.Directory = source.Generate.Directory
	}
	if source.Generate.Output != "" {
		destination.Generate.Output = source.Generate.Output
	}
	if source.Generate.Types != nil {
		destination.Generate.Types = append([]string(nil), source.Generate.Types...)
	}
	if source.Migrations.Directory != "" {
		destination.Migrations.Directory = source.Migrations.Directory
	}
	if source.Migrations.DatabaseEnv != "" {
		destination.Migrations.DatabaseEnv = source.Migrations.DatabaseEnv
	}
	if source.Migrations.HistorySchema != "" {
		destination.Migrations.HistorySchema = source.Migrations.HistorySchema
	}
	if source.Migrations.HistoryTable != "" {
		destination.Migrations.HistoryTable = source.Migrations.HistoryTable
	}
	if source.Migrations.LockKey != 0 {
		destination.Migrations.LockKey = source.Migrations.LockKey
	}
	if source.Migrations.MaximumRisk != "" {
		destination.Migrations.MaximumRisk = source.Migrations.MaximumRisk
	}
	if source.Migrations.Timeout != "" {
		destination.Migrations.Timeout = source.Migrations.Timeout
	}
}

func validateConfig(config Config) error {
	if strings.TrimSpace(config.Migrations.Directory) == "" {
		return fmt.Errorf("config migrations.directory is required")
	}
	if strings.TrimSpace(config.Migrations.DatabaseEnv) == "" {
		return fmt.Errorf("config migrations.databaseEnv is required")
	}
	if strings.TrimSpace(config.Migrations.HistorySchema) == "" {
		return fmt.Errorf("config migrations.historySchema is required")
	}
	if strings.TrimSpace(config.Migrations.HistoryTable) == "" {
		return fmt.Errorf("config migrations.historyTable is required")
	}
	if _, err := parseRisk(config.Migrations.MaximumRisk); err != nil {
		return fmt.Errorf("config migrations.maximumRisk: %w", err)
	}
	timeout, err := time.ParseDuration(config.Migrations.Timeout)
	if err != nil || timeout <= 0 {
		return fmt.Errorf("config migrations.timeout must be a positive duration")
	}
	return nil
}
