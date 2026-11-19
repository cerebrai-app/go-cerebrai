package brainconfig

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"

	"go.yaml.in/yaml/v4"
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
// Config.Get, Set and Unset take a Scope saying where key paths start:
// TopLevel, the file's top level, for brainctl, or ExtensionSettings, one
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
// key leads to, or nil if a key is missing or a key before the last has no
// value. It is an error if a key before the last leads to something other
// than a mapping or null.
func lookup(m *yaml.Node, keys []string) (*yaml.Node, error) {
	n := m
	for i, key := range keys {
		// A key with no value ("env:") is an empty mapping to set and unset,
		// so the keys under it are not set, rather than an error.
		if isNull(n) {
			return nil, nil
		}
		if n.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("%s is not a mapping, so it has no %s", strings.Join(keys[:i], "/"), key)
		}
		if n = mergedValue(n, key); n == nil {
			return nil, nil
		}
	}
	return n, nil
}

// mergedValue is the value of key in the mapping m as YAML reads it: m's own
// key, or else one m takes from the mappings its merge key (<<: *base, or
// <<: [*a, *b]) refers to, in order. nil if there is none.
func mergedValue(m *yaml.Node, key string) *yaml.Node {
	if v := mappingValue(m, key); v != nil {
		return v
	}
	merge := mergeValue(m)
	if merge == nil {
		return nil
	}
	sources := []*yaml.Node{merge}
	if merge.Kind == yaml.SequenceNode {
		sources = merge.Content
	}
	for _, src := range sources {
		if src.Kind == yaml.AliasNode {
			src = src.Alias
		}
		if src != nil && src.Kind == yaml.MappingNode {
			if v := mergedValue(src, key); v != nil {
				return v
			}
		}
	}
	return nil
}

// isMergeKey reports whether k, a key in a mapping, is YAML's merge key: <<
// written plainly (or tagged !!merge). A quoted "<<" is an ordinary key.
func isMergeKey(k *yaml.Node) bool {
	return k.Kind == yaml.ScalarNode && k.ShortTag() == "!!merge"
}

// mergeValue is the value of the merge key in the mapping m (what <<: refers
// to), or nil if m has none.
func mergeValue(m *yaml.Node) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if isMergeKey(m.Content[i]) {
			return m.Content[i+1]
		}
	}
	return nil
}

// checkNoMerge returns an error if the mapping m, at the key path keys (for
// messages), has a merge key (<<: *base): adding or removing a key there
// could hide what it takes from the mapping it merges, or change that
// mapping for everything else that merges it.
func checkNoMerge(m *yaml.Node, keys []string) error {
	if m.Kind != yaml.MappingNode || mergeValue(m) == nil {
		return nil
	}
	where := strings.Join(keys, "/")
	if where == "" {
		where = "the top of its scope"
	}
	return fmt.Errorf("%s uses a YAML merge key (<<), so changing it here could hide what it inherits: edit the file by hand", where)
}

// Scope is where in the file a key path starts: TopLevel, or the settings
// of one extension, from ExtensionSettings.
type Scope struct {
	// desc names the scope in messages.
	desc string
	// find returns the mapping key paths start from, in the file's top-level
	// mapping root, checking that it is one the caller can use.
	find func(root *yaml.Node) (*yaml.Node, error)
	// check returns an error if the value at keys may not be changed to
	// value (nil for unset).
	check func(keys []string, value *string) error
}

// TopLevel is the scope of the file's top level, which brainctl owns, and
// where the user's own keys go. Any key can be read through it, but
// schema_version and extensions cannot be changed: the first is brainctl's to
// manage, the second holds the extensions' settings, which they change
// themselves.
var TopLevel = Scope{
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

// ExtensionSettings is the scope of the settings of the extension with ID id,
// whose newest format the caller reads is schemaVersion: they must exist (see
// AddExtension) and be in a format the caller reads (see DecodeExtension), or
// Get, Set and Unset fail. schema_version cannot be changed through it, since
// the extension manages it, and name and bin must stay valid (see CheckName
// and CheckBin).
func ExtensionSettings(id string, schemaVersion int) Scope {
	return Scope{
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

// Get is the value at keyPath in scope s. A scalar's value is its text; a
// mapping or list is its YAML, in block style and ending in a newline. It is
// an ErrNotSet error if keyPath leads to nothing, or s is an extension's
// settings and it has none, and an error if keyPath passes through something
// that is not a mapping, or s's settings are not ones the caller reads.
func (c Config) Get(s Scope, keyPath string) (string, error) {
	return getValue(c.root, s, keyPath)
}

// getValue is the value at keyPath in scope s of the file whose top-level
// mapping is root.
func getValue(root *yaml.Node, s Scope, keyPath string) (string, error) {
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

// Set sets the value at keyPath in scope s of the config file at path to the
// string value, creating the mappings on the way that are missing.
//
// The value is stored as a string, quoted where it would otherwise read as
// another type, so "true" or "123" reads back as the same text. A value that
// is a mapping, a list or a reference to another value is never replaced, and
// keyPath cannot pass through a value that is not a mapping: either would lose
// what is there. Keys that s does not let be changed are refused (see
// TopLevel and ExtensionSettings).
//
// changed is false, and nothing is written, if the value is already set.
// Other content and comments are kept, including a comment on the value
// replaced; blank lines are not.
func Set(path string, s Scope, keyPath, value string) (changed bool, err error) {
	return changeValue(path, s, keyPath, &value)
}

// Unset removes the key at keyPath in scope s of the config file at path,
// with its value. A mapping or list goes with everything in it; a mapping left
// empty is kept. Keys that s does not let be changed are refused, as for Set.
// changed is false, and nothing is written, if the key is not there; keyPath
// passing through a value that is not a mapping is an error. Other content and
// comments are kept, apart from the removed key's own.
func Unset(path string, s Scope, keyPath string) (changed bool, err error) {
	return changeValue(path, s, keyPath, nil)
}

// changeValue sets the value at keyPath in scope s of the config file at path
// to *value, or removes it if value is nil. It applies the change to a parse
// of the file first, which decides whether there is anything to do and finds
// any error before anything is written, then to the document it writes.
func changeValue(path string, s Scope, keyPath string, value *string) (changed bool, err error) {
	keys, err := splitKeyPath(keyPath)
	if err != nil {
		return false, err
	}
	if err := s.check(keys, value); err != nil {
		return false, err
	}
	unlock, err := lockExisting(path)
	if err != nil {
		return false, err
	}
	defer unlock()
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
// creating the mappings on the way that are missing, as Set
// describes. changed is false if the value is already set.
func setIn(m *yaml.Node, keys []string, value string) (changed bool, err error) {
	last := len(keys) - 1
	for i, key := range keys[:last] {
		if err := checkNoMerge(m, keys[:i]); err != nil {
			return false, err
		}
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
	if err := checkNoMerge(m, keys[:last]); err != nil {
		return false, err
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
		// Keep an anchor (a: &x old): other keys that refer to it (b: *x)
		// take the new value too, as the alias says, rather than refer to
		// nothing and make the file unreadable.
		*v = yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value, Anchor: v.Anchor,
			HeadComment: v.HeadComment, LineComment: v.LineComment, FootComment: v.FootComment}
	}
	return true, nil
}

// unsetIn removes the key at keys from the mapping m, with its value.
// changed is false if it is not there.
func unsetIn(m *yaml.Node, keys []string) (changed bool, err error) {
	last := len(keys) - 1
	// Through the mappings' own keys only, not ones taken through a merge
	// key: removing one of those would change the mapping it came from.
	parent := m
	for i, key := range keys[:last] {
		if err := checkNoMerge(parent, keys[:i]); err != nil {
			return false, err
		}
		if parent = mappingValue(parent, key); parent == nil || isNull(parent) {
			return false, nil
		}
		if parent.Kind != yaml.MappingNode {
			return false, fmt.Errorf("%s is not a mapping, so it has no %s", strings.Join(keys[:i+1], "/"), keys[i+1])
		}
	}
	if err := checkNoMerge(parent, keys[:last]); err != nil {
		return false, err
	}
	for i := 0; i+1 < len(parent.Content); i += 2 {
		if !isMergeKey(parent.Content[i]) && parent.Content[i].Value == keys[last] {
			if anchor := findAnchor(parent.Content[i : i+2]...); anchor != "" {
				return false, fmt.Errorf("%s holds the anchor &%s, which other keys may refer to as *%s, so removing it "+
					"could make the file unreadable: edit it by hand", strings.Join(keys, "/"), anchor, anchor)
			}
			parent.Content = slices.Delete(parent.Content, i, i+2)
			return true, nil
		}
	}
	return false, nil
}

// findAnchor is the first anchor (&name) in the nodes or anything in them,
// or "".
func findAnchor(nodes ...*yaml.Node) string {
	for _, n := range nodes {
		if n.Anchor != "" {
			return n.Anchor
		}
		if a := findAnchor(n.Content...); a != "" {
			return a
		}
	}
	return ""
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
