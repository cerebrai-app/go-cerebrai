// Package config reads and writes the project config file that init creates:
// <dir>/.<name>/config.yaml in the directory the program runs in, where name
// is the program name without its "ctl" suffix (.brain for brainctl, .work
// for workctl).
//
// The file is YAML and always carries schema_version, so a later release can
// tell an older file apart and migrate it, and an older release refuses a
// file written by a newer one instead of misreading it.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

// SchemaVersion is the config file format this release writes and the newest
// one it reads. Raise it when a change would make older releases misread the
// file.
const SchemaVersion = 1

// FileName is the config file's name inside the config directory.
const FileName = "config.yaml"

// Config is the content of the config file.
type Config struct {
	// SchemaVersion is the format of the file; see the SchemaVersion constant.
	SchemaVersion int `yaml:"schema_version"`
}

// Default is the config init writes to a new file.
func Default() Config {
	return Config{SchemaVersion: SchemaVersion}
}

// DirName is the config directory for the program called name: ".brain" for
// brainctl.
func DirName(name string) string {
	return "." + strings.TrimSuffix(name, "ctl")
}

// Path is the config file of the program called name in the project directory dir.
func Path(dir, name string) string {
	return filepath.Join(dir, DirName(name), FileName)
}

// Init makes sure the config file at path exists and is one this release can
// read. A missing file is created, with its directory, from Default and
// created is true. An existing file is read and checked but never changed.
func Init(path string) (cfg Config, created bool, err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Config{}, false, err
	}
	data, err := Marshal(Default())
	if err != nil {
		return Config{}, false, err
	}
	// O_EXCL, so a file that appears between a check and the write is never
	// overwritten.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, fs.ErrExist) {
		cfg, err := Load(path)
		return cfg, false, err
	}
	if err != nil {
		return Config{}, false, err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return Config{}, false, fmt.Errorf("writing %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return Config{}, false, fmt.Errorf("writing %s: %w", path, err)
	}
	return Default(), true, nil
}

// Load reads the config file at path.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	cfg, err := Unmarshal(data)
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// Marshal encodes cfg as the config file's YAML.
func Marshal(cfg Config) ([]byte, error) {
	return yaml.Marshal(cfg)
}

// Unmarshal decodes the config file's YAML and checks its schema version.
// Unknown keys are an error, so a typo is not silently ignored.
func Unmarshal(data []byte) (Config, error) {
	var cfg Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		if errors.Is(err, io.EOF) {
			return Config{}, errors.New("config file is empty")
		}
		return Config{}, fmt.Errorf("invalid config file: %w", err)
	}
	switch {
	case cfg.SchemaVersion == 0:
		return Config{}, errors.New("config file has no schema_version")
	case cfg.SchemaVersion < 0:
		return Config{}, fmt.Errorf("config file has invalid schema_version %d", cfg.SchemaVersion)
	case cfg.SchemaVersion > SchemaVersion:
		return Config{}, fmt.Errorf("config file has schema_version %d, but this release reads only up to %d: upgrade to a newer release",
			cfg.SchemaVersion, SchemaVersion)
	}
	return cfg, nil
}
