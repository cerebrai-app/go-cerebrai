package brainconfig

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Values inside an extension's settings, addressed by a key path: keys
// separated by "/", each naming a key in the mapping the previous one leads
// to. env/client_id is the client_id key in the env mapping:
//
//	extensions:
//	  app.cerebrai.workctl:
//	    schema_version: 1
//	    env:
//	      client_id: abc123

// ErrNotSet is the error, matched with errors.Is, for a key path that leads
// to no value.
var ErrNotSet = errors.New("not set")

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

// ExtensionValue is the value at keyPath in the settings of the extension with
// ID id, whose newest format the caller reads is schemaVersion. A scalar's
// value is its text; a mapping or list is its YAML, in block style and
// ending in a newline. It
// is an ErrNotSet error if the extension has no settings or keyPath leads to
// nothing, and an error if the settings are not ones the caller reads (see
// DecodeExtension) or keyPath passes through something that is not a mapping.
func (c Config) ExtensionValue(id string, schemaVersion int, keyPath string) (string, error) {
	keys, err := splitKeyPath(keyPath)
	if err != nil {
		return "", err
	}
	node, ok := c.extensions[id]
	if !ok {
		return "", fmt.Errorf("%s in the settings for extension %s: %w", keyPath, id, ErrNotSet)
	}
	if _, err := extensionHeader(id, &node, schemaVersion); err != nil {
		return "", err
	}
	v, err := lookup(&node, keys)
	if err != nil {
		return "", fmt.Errorf("settings for extension %s: %w", id, err)
	}
	if v == nil {
		return "", fmt.Errorf("%s in the settings for extension %s: %w", keyPath, id, ErrNotSet)
	}
	if v.Kind == yaml.ScalarNode {
		if v.Tag == "!!null" {
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

// SetExtensionValue sets the value at keyPath in the settings of the extension
// with ID id, in the config file at path, to the string value, creating the
// mappings on the way that are missing. schemaVersion is the newest format of
// the settings the caller reads: they must exist (see AddExtension), and be
// ones it reads, before anything is written.
//
// The value is stored as a string, quoted where it would otherwise read as
// another type, so "true" or "123" reads back as the same text. A value that
// is a mapping or a list is never replaced, and keyPath cannot pass through
// a value that is not a mapping: either would lose what is there.
// schema_version cannot be set, since the extension manages it, and name and
// bin must be valid (see CheckName and CheckBin).
//
// changed is false, and nothing is written, if the value is already set.
// Other content and comments are kept, including a comment on the value
// replaced; blank lines are not.
func SetExtensionValue(path, id string, schemaVersion int, keyPath, value string) (changed bool, err error) {
	keys, err := splitKeyPath(keyPath)
	if err != nil {
		return false, err
	}
	if len(keys) == 1 {
		switch keys[0] {
		case "schema_version":
			return false, errors.New("schema_version cannot be set: the extension manages it")
		case "name":
			err = CheckName(value)
		case "bin":
			err = CheckBin(value)
		}
		if err != nil {
			return false, err
		}
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
		return false, fmt.Errorf("%s: no settings for extension %s", path, id)
	}
	if _, err := extensionHeader(id, &node, schemaVersion); err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	if v, err := lookup(&node, keys); err == nil && v != nil && v.Kind == yaml.ScalarNode && v.Tag != "!!null" && v.Value == value {
		return false, nil
	}

	err = editConfig(path, data, func(root *yaml.Node) error {
		m := mappingValue(mappingValue(root, "extensions"), id)
		last := len(keys) - 1
		for i, key := range keys[:last] {
			next := mappingValue(m, key)
			if next == nil {
				next = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
				m.Content = append(m.Content, scalar(key), next)
			} else if next.Kind != yaml.MappingNode && !isNull(next) {
				return fmt.Errorf("settings for extension %s: %s is not a mapping, so it has no %s",
					id, strings.Join(keys[:i+1], "/"), keys[i+1])
			} else if isNull(next) {
				// "env:" with no value yet: make it the mapping.
				makeBlock(next, yaml.MappingNode, "!!map")
			}
			m = next
		}
		v := mappingValue(m, keys[last])
		switch {
		case v == nil:
			m.Content = append(m.Content, scalar(keys[last]), scalar(value))
		case v.Kind == yaml.MappingNode || v.Kind == yaml.SequenceNode || v.Kind == yaml.AliasNode:
			return fmt.Errorf("settings for extension %s: %s holds a %s, which setting a value would replace; set the keys inside it instead",
				id, keyPath, kindName(v.Kind))
		default:
			*v = yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value,
				HeadComment: v.HeadComment, LineComment: v.LineComment, FootComment: v.FootComment}
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return true, nil
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
