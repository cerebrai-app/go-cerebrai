package config

import (
	"os"
	"path/filepath"
	"runtime"
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
	const content = "# mine\nschema_version: 1\nextensions:\n    workctl: {a: 1}\n"
	path := writeConfig(t, content)
	if created, err := Init(path); err != nil || created {
		t.Fatalf("Init: %v, %v", created, err)
	}
	if got := readFile(t, path); got != content {
		t.Errorf("file changed to %q", got)
	}
}

func TestInitRejectsBadExistingFile(t *testing.T) {
	path := writeConfig(t, "schema_version: 99\n")
	if _, err := Init(path); err == nil || !strings.Contains(err.Error(), "upgrade") {
		t.Errorf("Init: %v", err)
	}
}

func TestAddExtension(t *testing.T) {
	for _, tt := range []struct{ name, in, want string }{
		{"new file", "schema_version: 1\n", "schema_version: 1\nextensions:\n  workctl: {}\n"},
		{"keeps comments and other extensions",
			"# my brain\nschema_version: 1 # format\n\nextensions:\n  other:\n    x: 1 # keep\n",
			// Blank lines are not kept: yaml.v3 does not record them.
			"# my brain\nschema_version: 1 # format\nextensions:\n  other:\n    x: 1 # keep\n  workctl: {}\n"},
		{"empty extensions", "schema_version: 1\nextensions:\n", "schema_version: 1\nextensions:\n  workctl: {}\n"},
		{"null extensions", "schema_version: 1\nextensions: null\n", "schema_version: 1\nextensions:\n  workctl: {}\n"},
		{"flow extensions", "schema_version: 1\nextensions: {}\n", "schema_version: 1\nextensions:\n  workctl: {}\n"},
	} {
		path := writeConfig(t, tt.in)
		if added, err := AddExtension(path, "workctl"); err != nil || !added {
			t.Errorf("%s: AddExtension: %v, %v", tt.name, added, err)
			continue
		}
		if got := readFile(t, path); got != tt.want {
			t.Errorf("%s: file = %q, want %q", tt.name, got, tt.want)
		}
		cfg, err := Load(path)
		if _, ok := cfg.Extensions["workctl"]; err != nil || !ok {
			t.Errorf("%s: Load: %+v, %v", tt.name, cfg, err)
		}
	}
}

func TestAddExtensionKeepsExisting(t *testing.T) {
	const content = "schema_version: 1\nextensions:\n    workctl:\n        setting: true\n"
	path := writeConfig(t, content)
	if added, err := AddExtension(path, "workctl"); err != nil || added {
		t.Fatalf("AddExtension: %v, %v", added, err)
	}
	if got := readFile(t, path); got != content {
		t.Errorf("file changed to %q", got)
	}
}

func TestAddExtensionRejectsBadFile(t *testing.T) {
	const content = "schema_version: 1\nbogus: 1\n"
	path := writeConfig(t, content)
	if _, err := AddExtension(path, "workctl"); err == nil {
		t.Error("AddExtension accepted an invalid file")
	}
	if got := readFile(t, path); got != content {
		t.Errorf("file changed to %q", got)
	}
	if _, err := AddExtension(filepath.Join(t.TempDir(), "missing.yaml"), "workctl"); err == nil {
		t.Error("AddExtension accepted a missing file")
	}
}

func TestAddExtensionKeepsPermissions(t *testing.T) {
	path := writeConfig(t, "schema_version: 1\n")
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := AddExtension(path, "workctl"); err != nil {
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
		"schema_version: 1\nextensions:\n  workctl: {}\n  other: {x: [1, 2]}\n",
	} {
		if cfg, err := Unmarshal([]byte(in)); err != nil || cfg.SchemaVersion != 1 {
			t.Errorf("%q: %+v, %v", in, cfg, err)
		}
	}
	for in, wantErr := range map[string]string{
		"":                                 "empty",
		"# only a comment\n":               "empty",
		"other: 1\n":                       "invalid config file",
		"schema_version: 1\nextra: true\n": "invalid config file",
		"schema_version: one\n":            "invalid config file",
		"schema_version: 0\n":              "no schema_version",
		"schema_version: -1\n":             "invalid schema_version",
		"schema_version: 2\n":              "reads only up to 1",
		"[1, 2]\n":                         "invalid config file",
		"schema_version: 1\n---\nx: 1\n":   "single YAML document",
		"schema_version: 1\nextensions: [workctl]\n":     "invalid config file",
		"schema_version: 1\nextensions:\n  workctl: 1\n": "extensions.workctl must be a mapping",
		"schema_version: 1\nextensions:\n  workctl:\n":   "extensions.workctl must be a mapping",
	} {
		if _, err := Unmarshal([]byte(in)); err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Errorf("%q: got %v, want error containing %q", in, err, wantErr)
		}
	}
}
