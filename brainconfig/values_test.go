package brainconfig

import (
	"errors"
	"strings"
	"testing"
)

// Values inside an extension's settings, by key path.

const workctlID = "app.cerebrai.workctl"

// withSettings is a config file whose workctl settings are the lines body,
// indented under them.
func withSettings(body string) string {
	return "schema_version: 1\nextensions:\n  app.cerebrai.workctl:\n    schema_version: 1\n" + body
}

func TestSplitKeyPath(t *testing.T) {
	for in, want := range map[string]string{"a": "a", "env/client_id": "env|client_id", "a/b/c": "a|b|c", "a b/c.d": "a b|c.d"} {
		got, err := splitKeyPath(in)
		if err != nil || strings.Join(got, "|") != want {
			t.Errorf("%q: %q, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "/", "/a", "a/", "a//b"} {
		if _, err := splitKeyPath(in); err == nil || !strings.Contains(err.Error(), "invalid key path") {
			t.Errorf("%q: %v", in, err)
		}
	}
}

func TestGetExtensionSettings(t *testing.T) {
	cfg, err := Unmarshal([]byte(withSettings("    name: workctl\n    env:\n      client_id: abc123 # mine\n" +
		"      debug: \"true\"\n      empty:\n      list: [a, b]\n      nested: {deep: {x: 1}}\n    port: 8080\n")))
	if err != nil {
		t.Fatal(err)
	}
	for keyPath, want := range map[string]string{
		"env/client_id":     "abc123",
		"env/debug":         "true",
		"env/empty":         "",
		"port":              "8080",
		"name":              "workctl",
		"schema_version":    "1",
		"env/nested/deep/x": "1",
		"env/list":          "- a\n- b\n",
		"env/nested":        "deep:\n  x: 1\n",
		"env/nested/deep":   "x: 1\n",
	} {
		if got, err := cfg.Get(ExtensionSettings(workctlID, 1), keyPath); err != nil || got != want {
			t.Errorf("%s: %q, %v; want %q", keyPath, got, err, want)
		}
	}
	for keyPath, wantErr := range map[string]string{
		"missing":            "not set",
		"env/missing":        "not set",
		"env/nested/missing": "not set",
		"env/client_id/x":    "env/client_id is not a mapping, so it has no x",
		"env/list/0":         "env/list is not a mapping, so it has no 0",
		"port/x/y":           "port is not a mapping, so it has no x",
		"env//x":             "invalid key path",
	} {
		_, err := cfg.Get(ExtensionSettings(workctlID, 1), keyPath)
		if err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Errorf("%s: %v; want an error containing %q", keyPath, err, wantErr)
		}
		if notSet := wantErr == "not set"; errors.Is(err, ErrNotSet) != notSet {
			t.Errorf("%s: errors.Is(err, ErrNotSet) = %v, want %v", keyPath, !notSet, notSet)
		}
	}
	if _, err := cfg.Get(ExtensionSettings("com.example.other", 1), "x"); !errors.Is(err, ErrNotSet) {
		t.Errorf("extension without settings: %v", err)
	}
	if _, err := cfg.Get(ExtensionSettings(workctlID, 0), "env/client_id"); err == nil || !strings.Contains(err.Error(), "upgrade") {
		t.Errorf("settings newer than the caller reads: %v", err)
	}
	bad, err := Unmarshal([]byte("schema_version: 1\nextensions:\n  app.cerebrai.workctl: [not, a, mapping]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bad.Get(ExtensionSettings(workctlID, 1), "x"); err == nil || !strings.Contains(err.Error(), "must be a mapping") {
		t.Errorf("settings not a mapping: %v", err)
	}
}

func TestSetExtensionSettings(t *testing.T) {
	for _, tt := range []struct{ name, before, keyPath, value, after string }{
		{"new top key", "", "port", "8080",
			"    port: \"8080\"\n"},
		{"new nested key and its mapping", "", "env/client_id", "abc123",
			"    env:\n      client_id: abc123\n"},
		{"new key in an existing mapping", "    env:\n      a: b\n", "env/client_id", "abc123",
			"    env:\n      a: b\n      client_id: abc123\n"},
		{"deeper", "    env: {}\n", "env/x/y/z", "1",
			"    env: {x: {y: {z: \"1\"}}}\n"},
		{"replaces a value, keeping its comment", "    env:\n      client_id: old # mine\n", "env/client_id", "new",
			"    env:\n      client_id: new # mine\n"},
		{"replaces a null value", "    env:\n      client_id:\n", "env/client_id", "abc",
			"    env:\n      client_id: abc\n"},
		{"fills in a null mapping", "    env:\n", "env/client_id", "abc",
			"    env:\n      client_id: abc\n"},
		{"keeps the string type", "", "debug", "true",
			"    debug: \"true\"\n"},
		{"replacing keeps the string type", "    debug: false\n    port: 80\n", "debug", "true",
			"    debug: \"true\"\n    port: 80\n"},
		{"needs quoting", "", "x", "a: b # c",
			"    x: 'a: b # c'\n"},
		{"empty", "", "x", "",
			"    x: \"\"\n"},
		{"multi-line", "", "x", "line 1\nline 2",
			"    x: |-\n      line 1\n      line 2\n"},
		{"replaces name", "    name: workctl\n", "name", "My Work",
			"    name: My Work\n"},
		{"sets bin", "", "bin", "/opt/x/workctl",
			"    bin: /opt/x/workctl\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfig(t, withSettings(tt.before))
			changed, err := Set(path, ExtensionSettings(workctlID, 1), tt.keyPath, tt.value)
			if err != nil || !changed {
				t.Fatalf("Set: %v, %v", changed, err)
			}
			if got, want := readFile(t, path), withSettings(tt.after); got != want {
				t.Errorf("file = %q, want %q", got, want)
			}
			cfg, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if got, err := cfg.Get(ExtensionSettings(workctlID, 1), tt.keyPath); err != nil || got != tt.value {
				t.Errorf("reads back as %q, %v; want %q", got, err, tt.value)
			}
			// Setting the same value again changes nothing.
			if changed, err := Set(path, ExtensionSettings(workctlID, 1), tt.keyPath, tt.value); err != nil || changed {
				t.Errorf("again: %v, %v", changed, err)
			}
			if got, want := readFile(t, path), withSettings(tt.after); got != want {
				t.Errorf("file changed to %q", got)
			}
		})
	}
}

// Other content around the value, comments included, is kept.
func TestSetKeepsTheRest(t *testing.T) {
	const before = "# mine\nschema_version: 1\nowner: me\nextensions:\n" +
		"  app.cerebrai.workctl:\n    schema_version: 1 # format\n    name: workctl\n    color: blue\n" +
		"  com.example.tool: {schema_version: 3, env: {client_id: theirs}}\n"
	path := writeConfig(t, before)
	if _, err := Set(path, ExtensionSettings(workctlID, 1), "env/client_id", "ours"); err != nil {
		t.Fatal(err)
	}
	const want = "# mine\nschema_version: 1\nowner: me\nextensions:\n" +
		"  app.cerebrai.workctl:\n    schema_version: 1 # format\n    name: workctl\n    color: blue\n" +
		"    env:\n      client_id: ours\n" +
		"  com.example.tool: {schema_version: 3, env: {client_id: theirs}}\n"
	if got := readFile(t, path); got != want {
		t.Errorf("file = %q, want %q", got, want)
	}
}

func TestSetRefuses(t *testing.T) {
	body := "    env:\n      client_id: abc\n      list: [a]\n      nested: {x: 1}\n"
	for _, tt := range []struct{ name, content, keyPath, value, wantErr string }{
		{"replacing a mapping", withSettings(body), "env", "x", "env holds a mapping, which setting a value would replace"},
		{"replacing a list", withSettings(body), "env/list", "x", "env/list holds a list"},
		{"through a value", withSettings(body), "env/client_id/x", "y", "env/client_id is not a mapping, so it has no x"},
		{"through a list", withSettings(body), "env/list/x", "y", "env/list is not a mapping"},
		{"schema_version", withSettings(""), "schema_version", "2", "schema_version cannot be changed"},
		{"bad name", withSettings(""), "name", " x", "invalid name"},
		{"bad bin", withSettings(""), "bin", "bin/x", "invalid bin"},
		{"bad key path", withSettings(""), "a//b", "x", "invalid key path"},
		{"no settings", brainOnly, "env/client_id", "x", "no settings for extension app.cerebrai.workctl"},
		{"newer settings", "schema_version: 1\nextensions:\n  app.cerebrai.workctl: {schema_version: 2}\n", "x", "y", "upgrade"},
		{"bad settings", "schema_version: 1\nextensions:\n  app.cerebrai.workctl: {schema_version: 1, bin: a/b}\n", "x", "y", "invalid bin"},
		{"bad file", "schema_version: 2\n", "x", "y", "upgrade"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfig(t, tt.content)
			changed, err := Set(path, ExtensionSettings(workctlID, 1), tt.keyPath, tt.value)
			if err == nil || changed || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Set: %v, %v; want an error containing %q", changed, err, tt.wantErr)
			}
			if got := readFile(t, path); got != tt.content {
				t.Errorf("file changed to %q", got)
			}
		})
	}
	if _, err := Set(Path(t.TempDir()), ExtensionSettings(workctlID, 1), "x", "y"); !errors.Is(err, ErrNoConfig) {
		t.Errorf("missing file: %v", err)
	}
	// schema_version is only refused at the top of the settings: a key of
	// that name elsewhere is the extension's own.
	path := writeConfig(t, withSettings(""))
	if _, err := Set(path, ExtensionSettings(workctlID, 1), "env/schema_version", "2"); err != nil {
		t.Errorf("env/schema_version: %v", err)
	}
}

// get prints a mapping or list in block style, however it was written.
func TestGetBlockStyle(t *testing.T) {
	cfg, err := Unmarshal([]byte(withSettings("    a: {b: [1, {c: d}], e: f} # mine\n")))
	if err != nil {
		t.Fatal(err)
	}
	const want = "b:\n  - 1\n  - c: d\ne: f\n"
	if got, err := cfg.Get(ExtensionSettings(workctlID, 1), "a"); err != nil || got != want {
		t.Errorf("%q, %v; want %q", got, err, want)
	}
}

// A reference to another value (a YAML alias) is not replaced either: it
// would break what the user linked.
func TestSetRefusesAlias(t *testing.T) {
	content := withSettings("    base: &b {x: 1}\n    copy: *b\n")
	path := writeConfig(t, content)
	if changed, err := Set(path, ExtensionSettings(workctlID, 1), "copy", "y"); err == nil || changed || !strings.Contains(err.Error(), "copy holds a reference to another value") {
		t.Errorf("Set: %v, %v", changed, err)
	}
	if got := readFile(t, path); got != content {
		t.Errorf("file changed to %q", got)
	}
}

func TestUnsetExtensionSettings(t *testing.T) {
	for _, tt := range []struct{ name, before, keyPath, after string }{
		{"a value", "    port: \"80\"\n    env:\n      a: b\n", "port",
			"    env:\n      a: b\n"},
		{"a nested value, keeping its mapping", "    env:\n      client_id: abc # mine\n      a: b\n", "env/client_id",
			"    env:\n      a: b\n"},
		{"the last value in a mapping, keeping it empty", "    env:\n      client_id: abc\n", "env/client_id",
			"    env: {}\n"},
		{"a whole mapping", "    env:\n      client_id: abc\n      nested: {x: 1}\n    port: \"80\"\n", "env",
			"    port: \"80\"\n"},
		{"a list", "    list: [a, b]\n", "list", ""},
		{"a null value", "    env:\n", "env", ""},
		{"from a flow mapping", "    env: {a: 1, b: 2}\n", "env/a", "    env: {b: 2}\n"},
		{"name", "    name: workctl\n", "name", ""},
		{"bin", "    bin: workctl\n", "bin", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfig(t, withSettings(tt.before))
			if changed, err := Unset(path, ExtensionSettings(workctlID, 1), tt.keyPath); err != nil || !changed {
				t.Fatalf("Unset: %v, %v", changed, err)
			}
			if got, want := readFile(t, path), withSettings(tt.after); got != want {
				t.Errorf("file = %q, want %q", got, want)
			}
			cfg, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := cfg.Get(ExtensionSettings(workctlID, 1), tt.keyPath); !errors.Is(err, ErrNotSet) {
				t.Errorf("after unset: %v", err)
			}
			// Again: not there, nothing to do.
			if changed, err := Unset(path, ExtensionSettings(workctlID, 1), tt.keyPath); err != nil || changed {
				t.Errorf("again: %v, %v", changed, err)
			}
			if got, want := readFile(t, path), withSettings(tt.after); got != want {
				t.Errorf("file changed to %q", got)
			}
		})
	}
}

// Unsetting what is not there is not an error and writes nothing, whether the
// key or the mappings on the way to it are missing.
func TestUnsetNotThere(t *testing.T) {
	content := withSettings("    env:\n      a: b\n    empty:\n")
	for _, keyPath := range []string{"missing", "env/missing", "missing/x/y", "empty/x"} {
		path := writeConfig(t, content)
		if changed, err := Unset(path, ExtensionSettings(workctlID, 1), keyPath); err != nil || changed {
			t.Errorf("%s: %v, %v", keyPath, changed, err)
		}
		if got := readFile(t, path); got != content {
			t.Errorf("%s: file changed to %q", keyPath, got)
		}
	}
}

func TestUnsetRefuses(t *testing.T) {
	body := "    env:\n      client_id: abc\n      list: [a]\n"
	for _, tt := range []struct{ name, content, keyPath, wantErr string }{
		{"through a value", withSettings(body), "env/client_id/x", "env/client_id is not a mapping, so it has no x"},
		{"through a list", withSettings(body), "env/list/0", "env/list is not a mapping"},
		{"schema_version", withSettings(""), "schema_version", "schema_version cannot be changed"},
		{"bad key path", withSettings(""), "env/", "invalid key path"},
		{"no settings", brainOnly, "x", "no settings for extension app.cerebrai.workctl"},
		{"newer settings", "schema_version: 1\nextensions:\n  app.cerebrai.workctl: {schema_version: 2, x: y}\n", "x", "upgrade"},
		{"bad file", "schema_version: 2\n", "x", "upgrade"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfig(t, tt.content)
			changed, err := Unset(path, ExtensionSettings(workctlID, 1), tt.keyPath)
			if err == nil || changed || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Unset: %v, %v; want an error containing %q", changed, err, tt.wantErr)
			}
			if got := readFile(t, path); got != tt.content {
				t.Errorf("file changed to %q", got)
			}
		})
	}
	if _, err := Unset(Path(t.TempDir()), ExtensionSettings(workctlID, 1), "x"); !errors.Is(err, ErrNoConfig) {
		t.Errorf("missing file: %v", err)
	}
}

// The top level: the user's own keys can be read, set and unset; schema_version
// and extensions can be read but not changed.
func TestTopLevelValues(t *testing.T) {
	const content = "schema_version: 1 # format\nowner: me # mine\nextensions:\n  app.cerebrai.workctl: {schema_version: 1, name: workctl}\n"
	path := writeConfig(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	for keyPath, want := range map[string]string{
		"owner":                                "me",
		"schema_version":                       "1",
		"extensions/app.cerebrai.workctl/name": "workctl",
		"extensions":                           "app.cerebrai.workctl:\n  schema_version: 1\n  name: workctl\n",
	} {
		if got, err := cfg.Get(TopLevel, keyPath); err != nil || got != want {
			t.Errorf("Get(%s) = %q, %v; want %q", keyPath, got, err, want)
		}
	}
	if _, err := cfg.Get(TopLevel, "missing"); !errors.Is(err, ErrNotSet) || !strings.Contains(err.Error(), "missing in the config file: not set") {
		t.Errorf("Get(missing): %v", err)
	}
	if _, err := cfg.Get(TopLevel, "owner/x"); err == nil || !strings.Contains(err.Error(), "config file: owner is not a mapping") {
		t.Errorf("Get(owner/x): %v", err)
	}

	for _, tt := range []struct {
		keyPath string
		value   *string
		wantErr string
	}{
		{"schema_version", ptr("2"), "schema_version cannot be changed: brainctl manages it"},
		{"schema_version", nil, "schema_version cannot be changed"},
		{"extensions", ptr("x"), "extensions cannot be changed here"},
		{"extensions/app.cerebrai.workctl/name", ptr("x"), "extensions cannot be changed here"},
		{"extensions/app.cerebrai.workctl", nil, "extensions cannot be changed here"},
		{"owner/x", ptr("y"), "config file: owner is not a mapping"},
	} {
		var changed bool
		if tt.value != nil {
			changed, err = Set(path, TopLevel, tt.keyPath, *tt.value)
		} else {
			changed, err = Unset(path, TopLevel, tt.keyPath)
		}
		if err == nil || changed || !strings.Contains(err.Error(), tt.wantErr) {
			t.Errorf("%s: %v, %v; want an error containing %q", tt.keyPath, changed, err, tt.wantErr)
		}
	}
	if got := readFile(t, path); got != content {
		t.Fatalf("file changed to %q", got)
	}

	if changed, err := Set(path, TopLevel, "owner", "you"); err != nil || !changed {
		t.Errorf("Set(owner): %v, TopLevel, %v", changed, err)
	}
	if changed, err := Set(path, TopLevel, "sync/remote", "origin"); err != nil || !changed {
		t.Errorf("Set(sync/remote): %v, TopLevel, %v", changed, err)
	}
	if changed, err := Unset(path, TopLevel, "sync/remote"); err != nil || !changed {
		t.Errorf("Unset(sync/remote): %v, TopLevel, %v", changed, err)
	}
	if changed, err := Unset(path, TopLevel, "nothing"); err != nil || changed {
		t.Errorf("Unset(nothing): %v, TopLevel, %v", changed, err)
	}
	const want = "schema_version: 1 # format\nowner: you # mine\nextensions:\n  app.cerebrai.workctl: {schema_version: 1, name: workctl}\nsync: {}\n"
	if got := readFile(t, path); got != want {
		t.Errorf("file = %q, want %q", got, want)
	}
	if _, err := Set(Path(t.TempDir()), TopLevel, "owner", "me"); !errors.Is(err, ErrNoConfig) {
		t.Errorf("Set on a missing file: %v", err)
	}
}

func ptr(s string) *string { return &s }

// A key with no value ("env:") reads as an empty mapping, as set and unset
// treat it: the keys under it are not set, which is not an error.
func TestGetUnderNull(t *testing.T) {
	cfg, err := Unmarshal([]byte(withSettings("    env:\n    other: ~\n")))
	if err != nil {
		t.Fatal(err)
	}
	for _, keyPath := range []string{"env/client_id", "env/a/b", "other/x"} {
		if _, err := cfg.Get(ExtensionSettings(workctlID, 1), keyPath); !errors.Is(err, ErrNotSet) {
			t.Errorf("%s: %v; want ErrNotSet", keyPath, err)
		}
	}
	if got, err := cfg.Get(ExtensionSettings(workctlID, 1), "env"); err != nil || got != "" {
		t.Errorf("env: %q, %v", got, err)
	}
}

// Anchors (&x) survive set: keys that refer to them (*x) take the new value,
// as the alias says, and the file stays readable.
func TestSetKeepsAnchors(t *testing.T) {
	for _, tt := range []struct{ name, before, keyPath, value, after string }{
		{"anchored value", "    a: &x old\n    b: *x\n", "a", "new",
			"    a: &x new\n    b: *x\n"},
		{"anchored null filled in as a mapping", "    env: &e\n    copy: *e\n", "env/k", "v",
			"    env: &e\n      k: v\n    copy: *e\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfig(t, withSettings(tt.before))
			if _, err := Set(path, ExtensionSettings(workctlID, 1), tt.keyPath, tt.value); err != nil {
				t.Fatal(err)
			}
			if got, want := readFile(t, path), withSettings(tt.after); got != want {
				t.Errorf("file = %q, want %q", got, want)
			}
			cfg, err := Load(path)
			if err != nil {
				t.Fatalf("the file no longer reads: %v", err)
			}
			if got, err := cfg.Get(ExtensionSettings(workctlID, 1), tt.keyPath); err != nil || got != tt.value {
				t.Errorf("reads back as %q, %v", got, err)
			}
		})
	}
	// The alias follows the new value.
	path := writeConfig(t, withSettings("    a: &x old\n    b: *x\n"))
	if _, err := Set(path, ExtensionSettings(workctlID, 1), "a", "new"); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		B string `yaml:"b"`
	}
	if _, err := cfg.DecodeExtension(workctlID, 1, &s); err != nil || s.B != "new" {
		t.Errorf("b = %q, %v; want new", s.B, err)
	}
}

// unset refuses to remove an anchor, or anything holding one: aliases to it
// would then refer to nothing.
func TestUnsetRefusesAnchors(t *testing.T) {
	for _, keyPath := range []string{"a", "env", "env/inner"} {
		content := withSettings("    a: &x foo\n    b: *x\n    env:\n      inner: &y {k: v}\n    c: *y\n")
		path := writeConfig(t, content)
		changed, err := Unset(path, ExtensionSettings(workctlID, 1), keyPath)
		if err == nil || changed || !strings.Contains(err.Error(), "holds the anchor &") {
			t.Errorf("%s: %v, %v", keyPath, changed, err)
		}
		if got := readFile(t, path); got != content {
			t.Errorf("%s: file changed to %q", keyPath, got)
		}
	}
	// A key whose value only refers to an anchor can go.
	path := writeConfig(t, withSettings("    a: &x foo\n    b: *x\n"))
	if changed, err := Unset(path, ExtensionSettings(workctlID, 1), "b"); err != nil || !changed {
		t.Errorf("unset b: %v, %v", changed, err)
	}
	if _, err := Load(path); err != nil {
		t.Errorf("the file no longer reads: %v", err)
	}
}
