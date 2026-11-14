// Package brainconfig reads and writes the brain config file,
// .brain/config.yaml in a project directory. brainctl creates it, and
// extensions (workctl, and programs from anyone else) add their own settings
// to it, without brainctl having to know about them:
//
//	schema_version: 1
//	extensions:
//	  workctl:
//	    schema_version: 1
//	  example-tool:
//	    schema_version: 3
//	    anything: the extension likes
//
// The rules every extension follows, whether it uses this package or edits
// the YAML itself:
//
//   - Its settings live under extensions.<name>, where name is its program
//     name (lower case letters, digits, and ".", "-" or "_" between them), and
//     are a mapping holding schema_version: a positive integer that the
//     extension numbers on its own, independently of the file's.
//   - It changes nothing outside extensions.<name>: not the top-level keys,
//     not other extensions, and it keeps unknown content as it is.
//   - It refuses settings with a schema_version newer than it reads, instead
//     of misreading them.
//
// brainctl in turn checks only the top level: a schema_version it reads, no
// top-level keys it does not know, and extensions being a mapping. It never
// looks inside an extension's settings, so a broken or newer extension can
// never stop brainctl.
//
// The file's own schema_version lets a later release tell an older file apart
// and migrate it, and an older release refuses a file written by a newer one.
// Edits through this package keep the file's comments and the order of its
// keys, but not its blank lines, and re-indent it with two spaces.
package brainconfig

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"

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
	// extensions holds each extension's settings under its name. This
	// package does not look inside them when it reads the file: an extension
	// reads its own with DecodeExtension.
	extensions map[string]yaml.Node
}

// file is the YAML shape of Config.
type file struct {
	SchemaVersion int                  `yaml:"schema_version"`
	Extensions    map[string]yaml.Node `yaml:"extensions,omitempty"`
}

// HasExtension reports whether the extension called name has settings.
func (c Config) HasExtension(name string) bool {
	_, ok := c.extensions[name]
	return ok
}

// ExtensionNames are the names of the extensions with settings, sorted.
func (c Config) ExtensionNames() []string {
	return slices.Sorted(maps.Keys(c.extensions))
}

// validName matches an extension name: lower case letters and digits, with
// ".", "-" or "_" between them.
var validName = regexp.MustCompile(`^[a-z0-9]+([._-][a-z0-9]+)*$`)

// CheckName returns an error if name cannot be an extension name.
func CheckName(name string) error {
	if !validName.MatchString(name) {
		return fmt.Errorf("invalid extension name %q: use lower case letters and digits, with \".\", \"-\" or \"_\" between them", name)
	}
	if name == Base {
		return fmt.Errorf("%s is not an extension of itself", Base)
	}
	return nil
}

// DecodeExtension decodes the settings of the extension called name into v,
// normally a pointer to a struct with a SchemaVersion field tagged
// `yaml:"schema_version"`. schemaVersion is the newest format the caller
// reads: settings without a schema_version, with a newer one, or with keys
// that v has no field for are an error. ok is false, and v is untouched, if
// the extension has no settings.
func (c Config) DecodeExtension(name string, schemaVersion int, v any) (ok bool, err error) {
	node, ok := c.extensions[name]
	if !ok {
		return false, nil
	}
	if _, err := extensionSchemaVersion(name, &node, schemaVersion); err != nil {
		return true, err
	}
	// yaml.Node.Decode cannot reject unknown keys, so go through a Decoder.
	data, err := yaml.Marshal(&node)
	if err != nil {
		return true, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(v); err != nil {
		return true, fmt.Errorf("invalid extensions.%s: %w", name, err)
	}
	return true, nil
}

// extensionSchemaVersion is the schema_version of the extension settings in
// node, checked to be a mapping with a version from 1 to newest.
func extensionSchemaVersion(name string, node *yaml.Node, newest int) (int, error) {
	if node.Kind != yaml.MappingNode {
		return 0, fmt.Errorf("invalid config file: extensions.%s must be a mapping", name)
	}
	var ext struct {
		SchemaVersion int `yaml:"schema_version"`
	}
	if err := node.Decode(&ext); err != nil {
		return 0, fmt.Errorf("invalid extensions.%s: %w", name, err)
	}
	if err := checkSchemaVersion("extensions."+name+".schema_version", ext.SchemaVersion, newest); err != nil {
		return 0, err
	}
	return ext.SchemaVersion, nil
}

// checkSchemaVersion checks the schema version got, found at key, against
// newest, the newest this release reads.
func checkSchemaVersion(key string, got, newest int) error {
	switch {
	case got == 0:
		return fmt.Errorf("config file has no %s", key)
	case got < 0:
		return fmt.Errorf("config file has invalid %s %d", key, got)
	case got > newest:
		return fmt.Errorf("config file has %s %d, but this release reads only up to %d: upgrade to a newer release",
			key, got, newest)
	}
	return nil
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

// AddExtension adds extensions.<name>, holding only schema_version:
// schemaVersion, to the config file at path, which must exist and be valid.
// If the extension is already there it is never replaced and added is false;
// only its schema_version is checked (against schemaVersion as the newest),
// since the caller knows best what else to check, with DecodeExtension. Other
// content and comments are kept; blank lines are not.
func AddExtension(path, name string, schemaVersion int) (added bool, err error) {
	if err := CheckName(name); err != nil {
		return false, err
	}
	if schemaVersion < 1 {
		return false, fmt.Errorf("invalid schema version %d for extension %s: it must be at least 1", schemaVersion, name)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	cfg, err := Unmarshal(data)
	if err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	if node, ok := cfg.extensions[name]; ok {
		if _, err := extensionSchemaVersion(name, &node, schemaVersion); err != nil {
			return false, fmt.Errorf("%s: %w", path, err)
		}
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
	exts.Content = append(exts.Content, scalar(name), &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map",
		Content: []*yaml.Node{scalar("schema_version"),
			{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(schemaVersion)}}})

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

// scalar is a string scalar node.
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
// mapping with a schema_version this release reads, no top-level keys it does
// not know, and extensions, if there, a mapping. What each extension holds is
// not checked: see DecodeExtension.
func Unmarshal(data []byte) (Config, error) {
	var f file
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		if errors.Is(err, io.EOF) {
			return Config{}, errors.New("config file is empty")
		}
		return Config{}, fmt.Errorf("invalid config file: %w", err)
	}
	if err := dec.Decode(new(yaml.Node)); !errors.Is(err, io.EOF) {
		return Config{}, errors.New("invalid config file: it must hold a single YAML document")
	}
	if err := checkSchemaVersion("schema_version", f.SchemaVersion, SchemaVersion); err != nil {
		return Config{}, err
	}
	return Config{SchemaVersion: f.SchemaVersion, extensions: f.Extensions}, nil
}
