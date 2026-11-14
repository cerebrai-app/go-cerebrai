// Package brainconfig reads and writes the brain config file,
// .brain/config.yaml in the brain home: $BRAIN_HOME if set, otherwise the
// current directory (see Home). Keeping it there, next to the brain's data,
// means it is backed up or committed with the data. brainctl creates it, and
// extensions (workctl, and programs from anyone else) add their own settings
// to it, without brainctl having to know about them:
//
//	schema_version: 1
//	extensions:
//	  app.cerebrai.workctl:
//	    schema_version: 1
//	    bin: workctl
//	  com.example.tool:
//	    schema_version: 3
//	    bin: /opt/example/bin/tool
//	    anything: the extension likes
//
// Two keys in an extension's settings are standard, and ExtensionBase holds
// them: schema_version, and bin, the extension's executable, which brainctl
// can discover. bin is a bare name, looked up on PATH, or an absolute path
// (see CheckBin); a bare name suits a file that is committed or backed up,
// since it does not tie the file to one machine's layout.
//
// The rules every extension follows, whether it uses this package or edits
// the YAML itself:
//
//   - Its settings live under extensions, keyed by its ID: a reverse domain
//     name ending in the extension's name, such as app.cerebrai.workctl or
//     com.example.tool, from a domain its author controls, so two authors
//     never pick the same key. See CheckID for the exact form.
//   - Its settings are a mapping holding schema_version: a positive integer
//     that the extension numbers on its own, independently of the file's.
//   - Its settings hold bin, its executable, with AddExtensionBin, unless
//     the user has set one already.
//   - It changes nothing outside its own settings: not the top-level keys,
//     not other extensions, and it keeps unknown content as it is.
//   - It refuses settings with a schema_version newer than it reads, instead
//     of misreading them.
//
// brainctl in turn checks only the top level: a schema_version it reads, no
// top-level keys it does not know, and extensions being a mapping. Reading
// the file never looks inside an extension's settings, so a broken or newer
// extension can never stop brainctl; listing extensions reads each one's bin
// with Config.ExtensionBin and reports a bad one for that extension alone.
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
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

const (
	// SchemaVersion is the config file format this release writes and the
	// newest one it reads. Raise it when a change would make older releases
	// misread the file.
	SchemaVersion = 1
	// EnvVar names the environment variable holding the brain home.
	EnvVar = "BRAIN_HOME"
	// DirName is the config directory inside the brain home.
	DirName = ".brain"
	// FileName is the config file's name inside DirName.
	FileName = "config.yaml"
)

// Config is the content of the config file.
type Config struct {
	// SchemaVersion is the format of the file; see the SchemaVersion constant.
	SchemaVersion int `yaml:"schema_version"`
	// extensions holds each extension's settings under its ID. This
	// package does not look inside them when it reads the file: an extension
	// reads its own with DecodeExtension.
	extensions map[string]yaml.Node
}

// file is the YAML shape of Config.
type file struct {
	SchemaVersion int                  `yaml:"schema_version"`
	Extensions    map[string]yaml.Node `yaml:"extensions,omitempty"`
}

// ExtensionBase holds the standard keys of an extension's settings. An
// extension embeds it in the type it decodes its settings into, so that
// DecodeExtension, which rejects unknown keys, accepts them:
//
//	type settings struct {
//		brainconfig.ExtensionBase `yaml:",inline"`
//		Color string `yaml:"color"`
//	}
type ExtensionBase struct {
	// SchemaVersion is the format of the extension's settings. Each
	// extension numbers its own formats, independently of the file's.
	SchemaVersion int `yaml:"schema_version"`
	// Bin is the extension's executable, which brainctl can discover: a bare
	// name, looked up on PATH, or an absolute path. Empty if not set.
	Bin string `yaml:"bin,omitempty"`
}

// ExtensionBin is the bin of the extension with ID id: "" if the extension
// has no settings or they set no bin, and an error if its settings are not a
// mapping or bin is not one CheckBin accepts. The schema_version is not
// checked: brainctl lists extensions whatever their format.
func (c Config) ExtensionBin(id string) (string, error) {
	node, ok := c.extensions[id]
	if !ok {
		return "", nil
	}
	base, err := extensionBase(id, &node)
	return base.Bin, err
}

// CheckBin returns an error if bin cannot be an extension's executable: it
// must be a bare executable name, looked up on PATH, or an absolute path. A
// relative path with a directory in it would depend on the current directory.
// Absolute means absolute on this platform, or rooted at "/": the file may be
// shared between machines, so a Unix path is accepted on Windows too.
func CheckBin(bin string) error {
	switch {
	case strings.TrimSpace(bin) == "":
		return errors.New("invalid bin: empty")
	case bin != strings.TrimSpace(bin):
		return fmt.Errorf("invalid bin %q: leading or trailing space", bin)
	case filepath.IsAbs(bin) || path.IsAbs(bin):
		return nil
	case bin == "." || bin == ".." || strings.ContainsAny(bin, `/\`):
		return fmt.Errorf("invalid bin %q: use a bare executable name or an absolute path", bin)
	}
	return nil
}

// HasExtension reports whether the extension with ID id has settings.
func (c Config) HasExtension(id string) bool {
	_, ok := c.extensions[id]
	return ok
}

// ExtensionIDs are the IDs of the extensions with settings, sorted.
func (c Config) ExtensionIDs() []string {
	return slices.Sorted(maps.Keys(c.extensions))
}

// label is one label of a domain name: lower case letters and digits, with
// single hyphens between them.
const label = `[a-z0-9]+(-[a-z0-9]+)*`

// validID matches an extension ID: three or more labels separated by dots.
var validID = regexp.MustCompile(`^` + label + `(\.` + label + `){2,}$`)

// CheckID returns an error if id cannot be an extension ID. An ID is a
// reverse domain name followed by the extension's name, such as
// app.cerebrai.workctl for workctl, from cerebrai.app: three or more labels
// separated by dots, each of lower case letters and digits with single
// hyphens between them.
func CheckID(id string) error {
	if !validID.MatchString(id) {
		return fmt.Errorf("invalid extension ID %q: use a reverse domain name followed by the extension's name, "+
			"like com.example.tool, in lower case letters, digits and hyphens", id)
	}
	return nil
}

// DecodeExtension decodes the settings of the extension with ID id into v,
// normally a pointer to a struct that embeds ExtensionBase inline.
// schemaVersion is the newest format the caller reads: settings without a
// schema_version, with a newer one, with a bad bin, or with keys that v has
// no field for are an error. ok is false, and v is untouched, if
// the extension has no settings.
func (c Config) DecodeExtension(id string, schemaVersion int, v any) (ok bool, err error) {
	node, ok := c.extensions[id]
	if !ok {
		return false, nil
	}
	if _, err := extensionHeader(id, &node, schemaVersion); err != nil {
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
		return true, fmt.Errorf("invalid settings for extension %s: %w", id, err)
	}
	return true, nil
}

// extensionBase reads the standard keys of the extension settings in node,
// checking that they are a mapping and that bin, if there, is valid.
func extensionBase(id string, node *yaml.Node) (ExtensionBase, error) {
	if node.Kind != yaml.MappingNode {
		return ExtensionBase{}, fmt.Errorf("invalid config file: the settings for extension %s must be a mapping", id)
	}
	var base ExtensionBase
	if err := node.Decode(&base); err != nil {
		return ExtensionBase{}, fmt.Errorf("invalid settings for extension %s: %w", id, err)
	}
	if mappingValue(node, "bin") != nil {
		if err := CheckBin(base.Bin); err != nil {
			return ExtensionBase{}, fmt.Errorf("invalid settings for extension %s: %w", id, err)
		}
	}
	return base, nil
}

// extensionHeader is extensionBase, also checking that schema_version is
// from 1 to newest.
func extensionHeader(id string, node *yaml.Node, newest int) (ExtensionBase, error) {
	base, err := extensionBase(id, node)
	if err != nil {
		return ExtensionBase{}, err
	}
	if err := checkSchemaVersion(" for extension "+id, base.SchemaVersion, newest); err != nil {
		return ExtensionBase{}, err
	}
	return base, nil
}

// checkSchemaVersion checks the schema version got against newest, the
// newest this release reads. where follows "schema_version" in errors.
func checkSchemaVersion(where string, got, newest int) error {
	switch {
	case got == 0:
		return fmt.Errorf("config file has no schema_version%s", where)
	case got < 0:
		return fmt.Errorf("config file has invalid schema_version %d%s", got, where)
	case got > newest:
		return fmt.Errorf("config file has schema_version %d%s, but this release reads only up to %d: upgrade to a newer release",
			got, where, newest)
	}
	return nil
}

// Home is the brain home, which holds DirName: $BRAIN_HOME if set and not
// empty, otherwise the current directory. $BRAIN_HOME must name an existing
// directory, so a typo fails instead of starting a new brain elsewhere; a
// relative path is taken from the current directory. The result is absolute.
func Home() (string, error) {
	dir := os.Getenv(EnvVar)
	if dir == "" {
		return os.Getwd()
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("%s=%s: %w", EnvVar, dir, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("%s=%s: %w", EnvVar, dir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s=%s: not a directory", EnvVar, dir)
	}
	return abs, nil
}

// Path is the config file in the brain home dir, normally from Home.
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
	_, err = f.Write(data)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path)
		return false, fmt.Errorf("writing %s: %w", path, err)
	}
	return true, nil
}

// AddExtension adds settings for the extension with ID id, holding only
// schema_version: schemaVersion, to the config file at path, which must exist
// and be valid: an extension never creates the file, since that is brainctl
// init's job. A missing file is an ErrNoConfig error.
// If the extension is already there it is never replaced and added is false;
// only its schema_version is checked (against schemaVersion as the newest),
// since the caller knows best what else to check, with DecodeExtension. Other
// content and comments are kept; blank lines are not.
func AddExtension(path, id string, schemaVersion int) (added bool, err error) {
	if err := CheckID(id); err != nil {
		return false, err
	}
	if schemaVersion < 1 {
		return false, fmt.Errorf("invalid schema version %d for extension %s: it must be at least 1", schemaVersion, id)
	}
	data, err := readConfig(path)
	if err != nil {
		return false, err
	}
	cfg, err := Unmarshal(data)
	if err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	if node, ok := cfg.extensions[id]; ok {
		if _, err := extensionHeader(id, &node, schemaVersion); err != nil {
			return false, fmt.Errorf("%s: %w", path, err)
		}
		return false, nil
	}

	err = editConfig(path, data, func(root *yaml.Node) {
		exts := mappingValue(root, "extensions")
		if exts == nil {
			exts = &yaml.Node{}
			root.Content = append(root.Content, scalar("extensions"), exts)
		}
		makeBlock(exts, yaml.MappingNode, "!!map")
		exts.Content = append(exts.Content, scalar(id), &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map",
			Content: []*yaml.Node{scalar("schema_version"),
				{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(schemaVersion)}}})
	})
	if err != nil {
		return false, err
	}
	return true, nil
}

// AddExtensionBin sets bin, the executable of the extension with ID id, in
// its settings in the config file at path, unless they set one already: one
// the user wrote, say an absolute path, is never replaced. The settings must
// exist (see AddExtension), and the file must exist and be valid (a missing
// file is an ErrNoConfig error). bin is a bare name, looked up on PATH, or an
// absolute path; see CheckBin. added is false if a bin was set already. Other
// content and comments are kept; blank lines are not.
func AddExtensionBin(path, id, bin string) (added bool, err error) {
	if err := CheckID(id); err != nil {
		return false, err
	}
	if err := CheckBin(bin); err != nil {
		return false, err
	}
	data, err := readConfig(path)
	if err != nil {
		return false, err
	}
	cfg, err := Unmarshal(data)
	if err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	node, ok := cfg.extensions[id]
	if !ok {
		return false, fmt.Errorf("%s: no settings for extension %s to set its bin in", path, id)
	}
	if _, err := extensionBase(id, &node); err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	if mappingValue(&node, "bin") != nil {
		return false, nil
	}
	err = editConfig(path, data, func(root *yaml.Node) {
		ext := mappingValue(mappingValue(root, "extensions"), id)
		ext.Content = append(ext.Content, scalar("bin"), scalar(bin))
	})
	if err != nil {
		return false, err
	}
	return true, nil
}

// editConfig applies edit to the top-level mapping of data, the content of
// the config file at path, which Unmarshal has accepted, and writes the result
// to path with writeFile.
func editConfig(path string, data []byte, edit func(root *yaml.Node)) error {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("%s: invalid config file: %w", path, err)
	}
	// Unmarshal accepted the file, so it is one document holding a mapping.
	edit(doc.Content[0])
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	err := enc.Encode(&doc)
	if closeErr := enc.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = writeFile(path, buf.Bytes())
	}
	if err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// makeBlock makes n an empty node of kind and tag if it is anything else,
// such as a key with no value ("extensions:") or null, keeping its comments,
// and sets block style, even if it was written in flow style ({} or []).
func makeBlock(n *yaml.Node, kind yaml.Kind, tag string) {
	if n.Kind != kind {
		*n = yaml.Node{Kind: kind, Tag: tag,
			HeadComment: n.HeadComment, LineComment: n.LineComment, FootComment: n.FootComment}
	}
	n.Style = 0
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

// ErrNoConfig is the error, matched with errors.Is, for a config file that does
// not exist yet. Its message asks the user to run brainctl init.
var ErrNoConfig = errors.New("no brain config file: run brainctl init first")

// readConfig reads the config file at path, with ErrNoConfig if it is missing.
func readConfig(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%s: %w", path, ErrNoConfig)
	}
	return data, err
}

// Load reads the config file at path. A missing file is an ErrNoConfig error.
func Load(path string) (Config, error) {
	data, err := readConfig(path)
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
// not know, and extensions, if there, a mapping. What each extension holds
// is not checked: see DecodeExtension and ExtensionBin.
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
	if err := checkSchemaVersion("", f.SchemaVersion, SchemaVersion); err != nil {
		return Config{}, err
	}
	return Config{SchemaVersion: f.SchemaVersion, extensions: f.Extensions}, nil
}
