// Package config reads and writes the brain config file that init creates:
// .brain/config.yaml in the directory the program runs in.
//
// brainctl owns the file. Other programs, such as workctl, are extensions of
// brainctl: they share the same file and each keeps its own settings under
// extensions.<program name>, for example
//
//	schema_version: 1
//	extensions:
//	  workctl: {}
//
// The file always carries schema_version, so a later release can tell an
// older file apart and migrate it, and an older release refuses a file written
// by a newer one instead of misreading it. Edits keep the file's comments and
// the order of its keys, but not its blank lines, and re-indent it with two
// spaces.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"
)

const (
	// SchemaVersion is the config file format this release writes and the
	// newest one it reads. Raise it when a change would make older releases
	// misread the file.
	SchemaVersion = 1
	// Base is the program that owns the config file; every other program is
	// an extension of it.
	Base = "brainctl"
	// DirName is the config directory inside the project directory.
	DirName = ".brain"
	// FileName is the config file's name inside DirName.
	FileName = "config.yaml"
)

// Config is the content of the config file.
type Config struct {
	// SchemaVersion is the format of the file; see the SchemaVersion constant.
	SchemaVersion int `yaml:"schema_version"`
	// Extensions holds each extension's settings under its program name. The
	// brain config does not look inside them: each extension checks its own.
	Extensions map[string]yaml.Node `yaml:"extensions,omitempty"`
}

// Path is the config file in the project directory dir.
func Path(dir string) string {
	return filepath.Join(dir, DirName, FileName)
}

// Init makes sure the config file at path exists and is one this release can
// read. A missing file is created, with its directory, holding only
// schema_version, and created is true. An existing file is checked but never
// changed.
func Init(path string) (created bool, err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	data := fmt.Appendf(nil, "schema_version: %d\n", SchemaVersion)
	// O_EXCL, so a file that appears between a check and the write is never
	// overwritten.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, fs.ErrExist) {
		_, err := Load(path)
		return false, err
	}
	if err != nil {
		return false, err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return false, fmt.Errorf("writing %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return false, fmt.Errorf("writing %s: %w", path, err)
	}
	return true, nil
}

// AddExtension adds an empty extensions.<name> mapping to the config file at
// path, which must exist and be valid. It does nothing and returns false if
// the extension is already there; its settings are never replaced. Other
// content and comments are kept; blank lines are not.
func AddExtension(path, name string) (added bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	cfg, err := Unmarshal(data)
	if err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	if _, ok := cfg.Extensions[name]; ok {
		return false, nil
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return false, fmt.Errorf("%s: invalid config file: %w", path, err)
	}
	// Unmarshal accepted the file, so it is one document holding a mapping.
	root := doc.Content[0]
	exts := mappingValue(root, "extensions")
	if exts == nil {
		exts = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		root.Content = append(root.Content, scalar("extensions"), exts)
	} else if exts.Kind != yaml.MappingNode {
		// "extensions:" with no value, or "extensions: null".
		*exts = yaml.Node{Kind: yaml.MappingNode, Tag: "!!map",
			HeadComment: exts.HeadComment, LineComment: exts.LineComment, FootComment: exts.FootComment}
	}
	exts.Style = 0 // block style, even if it was written as {}
	exts.Content = append(exts.Content, scalar(name),
		&yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Style: yaml.FlowStyle})

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return false, err
	}
	if err := enc.Close(); err != nil {
		return false, err
	}
	if err := writeFile(path, buf.Bytes()); err != nil {
		return false, fmt.Errorf("writing %s: %w", path, err)
	}
	return true, nil
}

// mappingValue is the value of key in the mapping node m, or nil.
func mappingValue(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func scalar(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
}

// writeFile replaces the file at path with data, keeping its permissions. It
// writes a temporary file and renames it over path, so a failure never leaves
// a half-written config.
func writeFile(path string, data []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if err == nil {
		err = f.Chmod(info.Mode().Perm())
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		_ = os.Remove(tmp)
	}
	return err
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

// Unmarshal decodes the config file's YAML and checks it: it must be one
// mapping with a schema_version this release reads, no keys it does not know,
// and a mapping under each extension.
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
	if err := dec.Decode(new(yaml.Node)); !errors.Is(err, io.EOF) {
		return Config{}, errors.New("invalid config file: it must hold a single YAML document")
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
	for name, ext := range cfg.Extensions {
		if ext.Kind != yaml.MappingNode {
			return Config{}, fmt.Errorf("invalid config file: extensions.%s must be a mapping", name)
		}
	}
	return cfg, nil
}
