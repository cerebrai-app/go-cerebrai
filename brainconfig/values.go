package brainconfig

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Values in the config file, addressed by a key path: keys separated by "/",
// each naming a key in the mapping the previous one leads to. In an
// extension's settings, env/client_id is the client_id key in the env
// mapping:
//
//	owner: me
//	extensions:
//	  app.cerebrai.workctl:
//	    schema_version: 1
//	    env:
//	      client_id: abc123
//
// Value, SetValue and UnsetValue work on the file's top level, for brainctl;
// ExtensionValue, SetExtensionValue and UnsetExtensionValue work on one
// extension's settings, for that extension.

// ErrNotSet is the error, matched with errors.Is, for a key path that leads
// to no value.
var ErrNotSet = errors.New("not set")

// errNoSettings is the error for an extension with no settings: for getValue,
// its key paths are not set.
var errNoSettings = errors.New("no settings")

// splitKeyPath splits a key path into its keys: one or more, none empty.
func splitKeyPath(keyPath string) ([]string, error) {
	keys := strings.Split(keyPath, "/")
	if slices.Contains(keys, "") {
		return nil, fmt.Errorf("invalid key path %q: use keys separated by single slashes, like env/client_id", keyPath)
	}
	return keys, nil
}

// lookup follows keys from the mapping node m. It returns the node the last
// key leads to, or nil if a key is missing. It is an error if a key before
// the last leads to something other than a mapping.
func lookup(m *yaml.Node, keys []string) (*yaml.Node, error) {
	n := m
	for i, key := range keys {
		if n.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("%s is not a mapping, so it has no %s", strings.Join(keys[:i], "/"), key)
		}
		if n = mappingValue(n, key); n == nil {
			return nil, nil
		}
	}
	return n, nil
}

// scope is where in the file a key path starts: the top level, or one
// extension's settings.
type scope struct {
	// desc names the scope in messages.
	desc string
	// find returns the mapping key paths start from, in the file's top-level
	// mapping root, checking that it is one the caller can use.
	find func(root *yaml.Node) (*yaml.Node, error)
	// check returns an error if the value at keys may not be changed to
	// value (nil for unset).
	check func(keys []string, value *string) error
}

// topLevel is the scope of the file's top level, which brainctl owns.
// schema_version and extensions cannot be changed through it: the first is
// brainctl's to manage, the second the extensions'.
var topLevel = scope{
	desc: "config file",
	find: func(root *yaml.Node) (*yaml.Node, error) { return root, nil },
	check: func(keys []string, _ *string) error {
		switch keys[0] {
		case "schema_version":
			return errors.New("schema_version cannot be changed: brainctl manages it")
		case "extensions":
			return errors.New("extensions cannot be changed here: change an extension's settings with its own config command")
		}
		return nil
	},
}

// extensionScope is the scope of the settings of the extension with ID id,
// whose newest format the caller reads is schemaVersion. schema_version
// cannot be changed through it, since the extension manages it, and name and
// bin must stay valid.
func extensionScope(id string, schemaVersion int) scope {
	return scope{
		desc: "settings for extension " + id,
		find: func(root *yaml.Node) (*yaml.Node, error) {
			exts := mappingValue(root, "extensions")
			var m *yaml.Node
			if exts != nil && exts.Kind == yaml.MappingNode {
				m = mappingValue(exts, id)
			}
			if m == nil {
				return nil, fmt.Errorf("%w for extension %s", errNoSettings, id)
			}
			if _, err := extensionHeader(id, m, schemaVersion); err != nil {
				return nil, err
			}
			return m, nil
		},
		check: func(keys []string, value *string) error {
			if len(keys) != 1 {
				return nil
			}
			switch {
			case keys[0] == "schema_version":
				return errors.New("schema_version cannot be changed: the extension manages it")
			case value == nil:
				return nil
			case keys[0] == "name":
				return CheckName(*value)
			case keys[0] == "bin":
				return CheckBin(*value)
			}
			return nil
		},
	}
}

// Value is the value at keyPath from the file's top level, as ExtensionValue
// describes. Any key can be read, extensions and schema_version included.
func (c Config) Value(keyPath string) (string, error) {
	return getValue(c.root, topLevel, keyPath)
}

// ExtensionValue is the value at keyPath in the settings of the extension with
// ID id, whose newest format the caller reads is schemaVersion. A scalar's
// value is its text; a mapping or list is its YAML, in block style and ending
// in a newline. It is an ErrNotSet error if the extension has no settings or
// keyPath leads to nothing, and an error if the settings are not ones the
// caller reads (see DecodeExtension) or keyPath passes through something that
// is not a mapping.
func (c Config) ExtensionValue(id string, schemaVersion int, keyPath string) (string, error) {
	return getValue(c.root, extensionScope(id, schemaVersion), keyPath)
}

// getValue is the value at keyPath in scope s of the file whose top-level
// mapping is root.
func getValue(root *yaml.Node, s scope, keyPath string) (string, error) {
	keys, err := splitKeyPath(keyPath)
	if err != nil {
		return "", err
	}
	m, err := s.find(root)
	if errors.Is(err, errNoSettings) {
		return "", fmt.Errorf("%s in the %s: %w", keyPath, s.desc, ErrNotSet)
	}
	if err != nil {
		return "", err
	}
	v, err := lookup(m, keys)
	if err != nil {
		return "", fmt.Errorf("%s: %w", s.desc, err)
	}
	if v == nil {
		return "", fmt.Errorf("%s in the %s: %w", keyPath, s.desc, ErrNotSet)
	}
	if v.Kind == yaml.ScalarNode {
		if isNull(v) {
			return "", nil
		}
		return v.Value, nil
	}
	return encodeBlock(v), nil
}

// encodeBlock is the YAML of the mapping or list n in block style, indented
// two spaces like the config file, whatever style it was written in.
func encodeBlock(n *yaml.Node) string {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	// n came from a file that decoded, so it encodes: the errors cannot
	// happen, and the buffer cannot fail to take the output.
	_ = enc.Encode(blockStyle(n))
	_ = enc.Close()
	return buf.String()
}

// blockStyle is a copy of n with every mapping and list in block style.
func blockStyle(n *yaml.Node) *yaml.Node {
	c := *n
	if c.Kind == yaml.MappingNode || c.Kind == yaml.SequenceNode {
		c.Style &^= yaml.FlowStyle
	}
	c.Content = make([]*yaml.Node, len(n.Content))
	for i, child := range n.Content {
		c.Content[i] = blockStyle(child)
	}
	return &c
}

// SetValue sets the value at keyPath from the file's top level, in the config
// file at path, as SetExtensionValue describes. schema_version and extensions
// cannot be set: brainctl manages the first, and the second holds the
// extensions' settings, which they change themselves.
func SetValue(path, keyPath, value string) (changed bool, err error) {
	return changeValue(path, topLevel, keyPath, &value)
}

// SetExtensionValue sets the value at keyPath in the settings of the extension
// with ID id, in the config file at path, to the string value, creating the
// mappings on the way that are missing. schemaVersion is the newest format of
// the settings the caller reads: they must exist (see AddExtension), and be
// ones it reads, before anything is written.
//
// The value is stored as a string, quoted where it would otherwise read as
// another type, so "true" or "123" reads back as the same text. A value that
// is a mapping, a list or a reference to another value is never replaced, and
// keyPath cannot pass through a value that is not a mapping: either would lose
// what is there. schema_version cannot be set, since the extension manages
// it, and name and bin must be valid (see CheckName and CheckBin).
//
// changed is false, and nothing is written, if the value is already set.
// Other content and comments are kept, including a comment on the value
// replaced; blank lines are not.
func SetExtensionValue(path, id string, schemaVersion int, keyPath, value string) (changed bool, err error) {
	return changeValue(path, extensionScope(id, schemaVersion), keyPath, &value)
}

// UnsetValue removes the key at keyPath from the file's top level, in the
// config file at path, as UnsetExtensionValue describes. schema_version and
// extensions cannot be unset.
func UnsetValue(path, keyPath string) (changed bool, err error) {
	return changeValue(path, topLevel, keyPath, nil)
}

// UnsetExtensionValue removes the key at keyPath, with its value, from the
// settings of the extension with ID id, in the config file at path. A
// mapping or list goes with everything in it; a mapping left empty is kept.
// schemaVersion is as for SetExtensionValue, and schema_version cannot be
// unset. changed is false, and nothing is written, if the key is not there;
// keyPath passing through a value that is not a mapping is an error. Other
// content and comments are kept, apart from the removed key's own.
func UnsetExtensionValue(path, id string, schemaVersion int, keyPath string) (changed bool, err error) {
	return changeValue(path, extensionScope(id, schemaVersion), keyPath, nil)
}

// changeValue sets the value at keyPath in scope s of the config file at path
// to *value, or removes it if value is nil. It applies the change to a parse
// of the file first, which decides whether there is anything to do and finds
// any error before anything is written, then to the document it writes.
func changeValue(path string, s scope, keyPath string, value *string) (changed bool, err error) {
	keys, err := splitKeyPath(keyPath)
	if err != nil {
		return false, err
	}
	if err := s.check(keys, value); err != nil {
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
	change := func(root *yaml.Node) (bool, error) {
		m, err := s.find(root)
		if err != nil {
			return false, err
		}
		if value == nil {
			changed, err = unsetIn(m, keys)
		} else {
			changed, err = setIn(m, keys, *value)
		}
		if err != nil {
			return false, fmt.Errorf("%s: %w", s.desc, err)
		}
		return changed, nil
	}
	if changed, err := change(cfg.root); err != nil || !changed {
		if err != nil {
			err = fmt.Errorf("%s: %w", path, err)
		}
		return false, err
	}
	err = editConfig(path, data, func(root *yaml.Node) error {
		_, err := change(root)
		return err
	})
	if err != nil {
		return false, err
	}
	return true, nil
}

// setIn sets the value at keys from the mapping m to the string value,
// creating the mappings on the way that are missing, as SetExtensionValue
// describes. changed is false if the value is already set.
func setIn(m *yaml.Node, keys []string, value string) (changed bool, err error) {
	last := len(keys) - 1
	for i, key := range keys[:last] {
		next := mappingValue(m, key)
		switch {
		case next == nil:
			next = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			m.Content = append(m.Content, scalar(key), next)
		case isNull(next):
			// "env:" with no value yet: make it the mapping.
			makeBlock(next, yaml.MappingNode, "!!map")
		case next.Kind != yaml.MappingNode:
			return false, fmt.Errorf("%s is not a mapping, so it has no %s", strings.Join(keys[:i+1], "/"), keys[i+1])
		}
		m = next
	}
	v := mappingValue(m, keys[last])
	switch {
	case v == nil:
		m.Content = append(m.Content, scalar(keys[last]), scalar(value))
	case v.Kind == yaml.MappingNode || v.Kind == yaml.SequenceNode || v.Kind == yaml.AliasNode:
		return false, fmt.Errorf("%s holds a %s, which setting a value would replace; set the keys inside it instead",
			strings.Join(keys, "/"), kindName(v.Kind))
	case !isNull(v) && v.Value == value:
		return false, nil
	default:
		*v = yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value,
			HeadComment: v.HeadComment, LineComment: v.LineComment, FootComment: v.FootComment}
	}
	return true, nil
}

// unsetIn removes the key at keys from the mapping m, with its value.
// changed is false if it is not there.
func unsetIn(m *yaml.Node, keys []string) (changed bool, err error) {
	last := len(keys) - 1
	parent, err := lookup(m, keys[:last])
	if err != nil || parent == nil || isNull(parent) {
		return false, err
	}
	if parent.Kind != yaml.MappingNode {
		return false, fmt.Errorf("%s is not a mapping, so it has no %s", strings.Join(keys[:last], "/"), keys[last])
	}
	for i := 0; i+1 < len(parent.Content); i += 2 {
		if parent.Content[i].Value == keys[last] {
			parent.Content = slices.Delete(parent.Content, i, i+2)
			return true, nil
		}
	}
	return false, nil
}

// isNull reports whether n is a null scalar: a key with no value, null or ~.
func isNull(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && n.Tag == "!!null"
}

// kindName names a node kind for messages.
func kindName(k yaml.Kind) string {
	switch k {
	case yaml.MappingNode:
		return "mapping"
	case yaml.SequenceNode:
		return "list"
	}
	return "reference to another value"
}
