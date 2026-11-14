package brainconfig

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestPath(t *testing.T) {
	if got, want := Path("/p"), filepath.Join("/p", ".brain", "config.yaml"); got != want {
		t.Errorf("Path = %q, want %q", got, want)
	}
}

// writeConfig writes content to the config file of a new project directory
// and returns its path.
func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := Path(t.TempDir())
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestInitCreatesFile(t *testing.T) {
	path := Path(t.TempDir())
	created, err := Init(path)
	if err != nil || !created {
		t.Fatalf("Init: %v, %v", created, err)
	}
	if got, want := readFile(t, path), "schema_version: 1\n"; got != want {
		t.Errorf("file = %q, want %q", got, want)
	}
}

func TestInitKeepsExistingFile(t *testing.T) {
	const content = "# mine\nschema_version: 1\nextensions:\n    app.cerebrai.workctl: {schema_version: 1}\n"
	path := writeConfig(t, content)
	if created, err := Init(path); err != nil || created {
		t.Fatalf("Init: %v, %v", created, err)
	}
	if got := readFile(t, path); got != content {
		t.Errorf("file changed to %q", got)
	}
}

func TestAddExtension(t *testing.T) {
	for _, tt := range []struct{ name, in, want string }{
		{"new file", "schema_version: 1\n", "schema_version: 1\nextensions:\n  app.cerebrai.workctl:\n    schema_version: 1\n"},
		{"keeps comments and other extensions",
			"# my brain\nschema_version: 1 # format\n\nextensions:\n  other:\n    schema_version: 3 # keep\n",
			// Blank lines are not kept: yaml.v3 does not record them.
			"# my brain\nschema_version: 1 # format\nextensions:\n  other:\n    schema_version: 3 # keep\n  app.cerebrai.workctl:\n    schema_version: 1\n"},
		{"empty extensions", "schema_version: 1\nextensions:\n", "schema_version: 1\nextensions:\n  app.cerebrai.workctl:\n    schema_version: 1\n"},
		{"null extensions", "schema_version: 1\nextensions: null\n", "schema_version: 1\nextensions:\n  app.cerebrai.workctl:\n    schema_version: 1\n"},
		{"flow extensions", "schema_version: 1\nextensions: {}\n", "schema_version: 1\nextensions:\n  app.cerebrai.workctl:\n    schema_version: 1\n"},
	} {
		path := writeConfig(t, tt.in)
		if added, err := addSettings(path, "app.cerebrai.workctl", 1); err != nil || !added {
			t.Errorf("%s: AddExtension: %v, %v", tt.name, added, err)
			continue
		}
		if got := readFile(t, path); got != tt.want {
			t.Errorf("%s: file = %q, want %q", tt.name, got, tt.want)
		}
		cfg, err := Load(path)
		if err != nil || !cfg.HasExtension("app.cerebrai.workctl") {
			t.Errorf("%s: Load: %+v, %v", tt.name, cfg, err)
		}
	}
}

func TestAddExtensionKeepsExisting(t *testing.T) {
	const content = "schema_version: 1\nextensions:\n    app.cerebrai.workctl:\n        schema_version: 1 # mine\n"
	path := writeConfig(t, content)
	if added, err := addSettings(path, "app.cerebrai.workctl", 1); err != nil || added {
		t.Fatalf("AddExtension: %v, %v", added, err)
	}
	if got := readFile(t, path); got != content {
		t.Errorf("file changed to %q", got)
	}
}

func TestAddExtensionRejectsBadFile(t *testing.T) {
	const content = "schema_version: two\n"
	path := writeConfig(t, content)
	if _, err := addSettings(path, "app.cerebrai.workctl", 1); err == nil {
		t.Error("AddExtension accepted an invalid file")
	}
	if got := readFile(t, path); got != content {
		t.Errorf("file changed to %q", got)
	}
	missing := Path(t.TempDir())
	if _, err := addSettings(missing, "app.cerebrai.workctl", 1); !errors.Is(err, ErrNoConfig) {
		t.Errorf("AddExtension on a missing file: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(missing)); err == nil {
		t.Error("AddExtension made the config directory")
	}
	if _, err := Load(missing); !errors.Is(err, ErrNoConfig) {
		t.Errorf("Load on a missing file: %v", err)
	}
}

func TestAddExtensionKeepsPermissions(t *testing.T) {
	path := writeConfig(t, "schema_version: 1\n")
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := addSettings(path, "app.cerebrai.workctl", 1); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 && runtime.GOOS != "windows" {
		t.Errorf("mode = %v, want 0600", mode)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("left files behind: %v", entries)
	}
}

func TestUnmarshal(t *testing.T) {
	for _, in := range []string{
		"schema_version: 1\n",
		"schema_version: 1\nextensions:\n",
		"schema_version: 1\nextensions:\n  app.cerebrai.workctl: {schema_version: 1}\n  other: {schema_version: 7, x: [1, 2]}\n",
		// brainctl never looks inside extensions, so none of these stop it.
		"schema_version: 1\nextensions:\n  app.cerebrai.workctl: 1\n",
		"schema_version: 1\nextensions:\n  app.cerebrai.workctl:\n",
		"schema_version: 1\nextensions:\n  app.cerebrai.workctl: {}\n",
		"schema_version: 1\nextensions:\n  w: {schema_version: 99, x: [1]}\n",
		"schema_version: 1\nextensions:\n  Not A Name: {schema_version: x}\n",
	} {
		if cfg, err := Unmarshal([]byte(in)); err != nil || cfg.SchemaVersion != 1 {
			t.Errorf("%q: %+v, %v", in, cfg, err)
		}
	}
	for in, wantErr := range map[string]string{
		"":                               "empty",
		"# only a comment\n":             "empty",
		"other: 1\n":                     "no schema_version",
		"schema_version: one\n":          "invalid config file",
		"schema_version: 0\n":            "no schema_version",
		"schema_version: -1\n":           "invalid schema_version",
		"schema_version: 2\n":            "reads only up to 1",
		"[1, 2]\n":                       "invalid config file",
		"schema_version: 1\n---\nx: 1\n": "single YAML document",
		"schema_version: 1\nextensions: [workctl]\n": "invalid config file",
	} {
		if _, err := Unmarshal([]byte(in)); err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Errorf("%q: got %v, want error containing %q", in, err, wantErr)
		}
	}
}

type workctlSettings struct {
	SchemaVersion int `yaml:"schema_version"`
}

func TestDecodeExtension(t *testing.T) {
	cfg, err := Unmarshal([]byte("schema_version: 1\nextensions:\n" +
		"  app.cerebrai.workctl: {schema_version: 2}\n" +
		"  com.example.tool: {schema_version: 3, color: blue}\n"))
	if err != nil {
		t.Fatal(err)
	}
	var w workctlSettings
	if ok, err := cfg.DecodeExtension("app.cerebrai.workctl", 2, &w); err != nil || !ok || w.SchemaVersion != 2 {
		t.Errorf("app.cerebrai.workctl: %+v, %v, %v", w, ok, err)
	}
	// A third-party extension's own settings type.
	var third struct {
		SchemaVersion int    `yaml:"schema_version"`
		Color         string `yaml:"color"`
	}
	if ok, err := cfg.DecodeExtension("com.example.tool", 3, &third); err != nil || !ok || third.Color != "blue" {
		t.Errorf("com.example.tool: %+v, %v, %v", third, ok, err)
	}
	if got := cfg.ExtensionIDs(); !slices.Equal(got, []string{"app.cerebrai.workctl", "com.example.tool"}) {
		t.Errorf("ExtensionIDs = %q", got)
	}
	if ok, err := cfg.DecodeExtension("missing", 1, &w); err != nil || ok {
		t.Errorf("missing: %v, %v", ok, err)
	}
	if _, err := cfg.DecodeExtension("app.cerebrai.workctl", 1, &w); err == nil || !strings.Contains(err.Error(), "schema_version 2 for extension app.cerebrai.workctl, but this release reads only up to 1") {
		t.Errorf("too new: %v", err)
	}
	if _, err := cfg.DecodeExtension("com.example.tool", 3, &w); err == nil || !strings.Contains(err.Error(), "invalid settings for extension com.example.tool") {
		t.Errorf("unknown key: %v", err)
	}
	for in, wantErr := range map[string]string{
		"app.cerebrai.workctl: 1":                    "settings for extension app.cerebrai.workctl must be a mapping",
		"app.cerebrai.workctl:":                      "settings for extension app.cerebrai.workctl must be a mapping",
		"app.cerebrai.workctl: {}":                   "no schema_version for extension app.cerebrai.workctl",
		"app.cerebrai.workctl: {schema_version: -2}": "invalid schema_version -2 for extension app.cerebrai.workctl",
		"app.cerebrai.workctl: {schema_version: x}":  "invalid settings for extension app.cerebrai.workctl",
	} {
		cfg, err := Unmarshal([]byte("schema_version: 1\nextensions:\n  " + in + "\n"))
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if _, err := cfg.DecodeExtension("app.cerebrai.workctl", 1, &w); err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Errorf("%q: got %v, want error containing %q", in, err, wantErr)
		}
	}
}

func TestCheckID(t *testing.T) {
	for _, id := range []string{
		"app.cerebrai.workctl", "com.example.tool", "io.github.some-user.my-tool", "com.example.x2.sub.tool",
	} {
		if err := CheckID(id); err != nil {
			t.Errorf("%q: %v", id, err)
		}
	}
	for _, id := range []string{
		"", "workctl", "example.tool", "App.cerebrai.workctl", "com.example.-tool", "com.example.tool-",
		"com.example..tool", "com.example.my--tool", "com.example.my_tool", ".com.example.tool",
		"com.example.tool.", "com.example.a b", "com/example/tool",
	} {
		if err := CheckID(id); err == nil {
			t.Errorf("%q: accepted", id)
		}
	}
}

func TestAddExtensionKeepsOtherExtensions(t *testing.T) {
	// Another extension's settings, however odd, are kept as they are.
	path := writeConfig(t, "schema_version: 1\nextensions:\n  other: [not, a, mapping]\n")
	if added, err := addSettings(path, "com.example.tool", 4); err != nil || !added {
		t.Fatalf("AddExtension: %v, %v", added, err)
	}
	const want = "schema_version: 1\nextensions:\n  other: [not, a, mapping]\n  com.example.tool:\n    schema_version: 4\n"
	if got := readFile(t, path); got != want {
		t.Errorf("file = %q, want %q", got, want)
	}
}

// addSettings is AddExtension with only a schema version, reporting whether
// it added the settings: how most tests here use it.
func addSettings(path, id string, schemaVersion int) (bool, error) {
	added, err := AddExtension(path, id, ExtensionBase{SchemaVersion: schemaVersion})
	return slices.Contains(added, "schema_version"), err
}
