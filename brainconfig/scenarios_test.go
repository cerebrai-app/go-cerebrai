package brainconfig

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The config file scenarios through the package API, as brainctl and an
// extension (workctl or a third-party Go program) use it.

const brainOnly = "schema_version: 1\n"

// badFiles are config files that fail the top-level checks, which brainctl
// and every extension apply.
var badFiles = []struct{ name, content, wantErr string }{
	{"empty", "", "config file is empty"},
	{"only comments", "# nothing\n", "config file is empty"},
	{"not YAML", "schema_version: [\n", "invalid config file"},
	{"not a mapping", "- 1\n", "invalid config file"},
	{"two documents", "schema_version: 1\n---\nschema_version: 1\n", "single YAML document"},
	{"no schema version", "extensions: {}\n", "no schema_version"},
	{"schema version zero", "schema_version: 0\n", "no schema_version"},
	{"negative schema version", "schema_version: -1\n", "invalid schema_version -1"},
	{"schema version not a number", "schema_version: one\n", "invalid config file"},
	{"newer schema version", "schema_version: 2\n", "schema_version 2, but this release reads only up to 1: upgrade"},
	{"known key of the wrong type", "schema_version: 1\nextensions: yes\n", "invalid config file"},
	{"extensions not a mapping", "schema_version: 1\nextensions: [a]\n", "invalid config file"},
}

func TestInitRejectsBadFiles(t *testing.T) {
	for _, tt := range badFiles {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfig(t, tt.content)
			created, err := Init(path)
			if err == nil || created || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Init: %v, %v; want an error containing %q", created, err, tt.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), path) {
				t.Errorf("error %q does not name the file", err)
			}
			if got := readFile(t, path); got != tt.content {
				t.Errorf("file changed to %q", got)
			}
		})
	}
}

func TestAddExtensionRejectsBadFiles(t *testing.T) {
	for _, tt := range badFiles {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfig(t, tt.content)
			added, err := addSettings(path, "app.cerebrai.workctl", 1)
			if err == nil || added || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("AddExtension: %v, %v; want an error containing %q", added, err, tt.wantErr)
			}
			if got := readFile(t, path); got != tt.content {
				t.Errorf("file changed to %q", got)
			}
		})
	}
}

func TestInitFailsWhenConfigDirIsAFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, DirName), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if created, err := Init(Path(dir)); err == nil || created {
		t.Errorf("Init: %v, %v", created, err)
	}
}

func TestInitIgnoresExtensionSettings(t *testing.T) {
	for _, content := range []string{
		"schema_version: 1\nextensions:\n  com.example.tool: not a mapping\n",
		"schema_version: 1\nextensions:\n  com.example.tool:\n",
		"schema_version: 1\nextensions:\n  com.example.tool: {schema_version: 99, x: [1]}\n",
		"schema_version: 1\nextensions:\n  not an ID: {schema_version: x}\n",
	} {
		path := writeConfig(t, content)
		if created, err := Init(path); err != nil || created {
			t.Errorf("%q: Init: %v, %v", content, created, err)
		}
		if got := readFile(t, path); got != content {
			t.Errorf("%q: file changed to %q", content, got)
		}
	}
}

func TestNoConfig(t *testing.T) {
	path := Path(t.TempDir())
	for name, err := range map[string]error{
		"AddExtension": func() error { _, err := addSettings(path, "app.cerebrai.workctl", 1); return err }(),
		"Load":         func() error { _, err := Load(path); return err }(),
	} {
		if !errors.Is(err, ErrNoConfig) || !strings.Contains(err.Error(), "run brainctl init first") {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Dir(path)); err == nil {
		t.Error("made the config directory")
	}
	// Other read errors are not ErrNoConfig.
	existing := writeConfig(t, brainOnly)
	if _, err := Load(filepath.Dir(existing)); err == nil || errors.Is(err, ErrNoConfig) {
		t.Errorf("Load of a directory: %v", err)
	}
}

func TestAddExtensionRejectsBadSettings(t *testing.T) {
	for _, tt := range []struct{ name, settings, wantErr string }{
		{"not a mapping", "1", "must be a mapping"},
		{"null", "", "must be a mapping"},
		{"list", "[1]", "must be a mapping"},
		{"no schema version", "{}", "no schema_version for extension app.cerebrai.workctl"},
		{"schema version not a number", "{schema_version: one}", "invalid settings for extension app.cerebrai.workctl"},
		{"negative schema version", "{schema_version: -1}", "invalid schema_version -1 for extension app.cerebrai.workctl"},
		{"newer schema version", "{schema_version: 2}", "schema_version 2 for extension app.cerebrai.workctl, but this release reads only up to 1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			content := "schema_version: 1\nextensions:\n  app.cerebrai.workctl: " + tt.settings + "\n"
			path := writeConfig(t, content)
			added, err := addSettings(path, "app.cerebrai.workctl", 1)
			if err == nil || added || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("AddExtension: %v, %v; want an error containing %q", added, err, tt.wantErr)
			}
			if got := readFile(t, path); got != content {
				t.Errorf("file changed to %q", got)
			}
		})
	}
}

// AddExtension checks only the version of existing settings: unknown keys are
// the extension's to judge, with DecodeExtension.
func TestAddExtensionLeavesOtherKeysToTheExtension(t *testing.T) {
	const content = "schema_version: 1\nextensions:\n  com.example.tool: {schema_version: 1, color: blue}\n"
	path := writeConfig(t, content)
	if added, err := addSettings(path, "com.example.tool", 1); err != nil || added {
		t.Errorf("AddExtension: %v, %v", added, err)
	}
	if got := readFile(t, path); got != content {
		t.Errorf("file changed to %q", got)
	}
}

func TestAddExtensionRejectsBadIDs(t *testing.T) {
	for _, id := range []string{"", "workctl", "example.tool", "com.Example.tool", "com.example.my_tool", "com.example.tool."} {
		path := writeConfig(t, brainOnly)
		if added, err := addSettings(path, id, 1); err == nil || added || !strings.Contains(err.Error(), "invalid extension ID") {
			t.Errorf("%q: %v, %v", id, added, err)
		}
		if got := readFile(t, path); got != brainOnly {
			t.Errorf("%q: file changed to %q", id, got)
		}
	}
}

func TestAddExtensionRejectsBadSchemaVersions(t *testing.T) {
	for _, v := range []int{0, -1} {
		path := writeConfig(t, brainOnly)
		if added, err := addSettings(path, "com.example.tool", v); err == nil || added {
			t.Errorf("%d: %v, %v", v, added, err)
		}
		if got := readFile(t, path); got != brainOnly {
			t.Errorf("%d: file changed to %q", v, got)
		}
	}
}

// The whole life of a project's config file: brainctl creates it, workctl
// and a third-party extension add their settings in turn, each reads its own,
// and nothing changes when any of them runs again.
func TestConfigLifecycle(t *testing.T) {
	path := Path(t.TempDir())
	if created, err := Init(path); err != nil || !created {
		t.Fatalf("Init: %v, %v", created, err)
	}

	type workctlSettings struct {
		SchemaVersion int `yaml:"schema_version"`
	}
	type toolSettings struct {
		SchemaVersion int    `yaml:"schema_version"`
		Color         string `yaml:"color"`
	}
	exts := []struct {
		id      string
		version int
	}{{"app.cerebrai.workctl", 1}, {"com.example.tool", 3}}
	for _, ext := range exts {
		if added, err := addSettings(path, ext.id, ext.version); err != nil || !added {
			t.Fatalf("AddExtension(%s): %v, %v", ext.id, added, err)
		}
	}
	const want = "schema_version: 1\nextensions:\n" +
		"  app.cerebrai.workctl:\n    schema_version: 1\n" +
		"  com.example.tool:\n    schema_version: 3\n"
	if got := readFile(t, path); got != want {
		t.Fatalf("file = %q, want %q", got, want)
	}

	// Running everything again changes nothing.
	if created, err := Init(path); err != nil || created {
		t.Errorf("Init again: %v, %v", created, err)
	}
	for _, ext := range exts {
		if added, err := addSettings(path, ext.id, ext.version); err != nil || added {
			t.Errorf("AddExtension(%s) again: %v, %v", ext.id, added, err)
		}
	}
	if got := readFile(t, path); got != want {
		t.Errorf("file changed to %q", got)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SchemaVersion != SchemaVersion {
		t.Errorf("SchemaVersion = %d", cfg.SchemaVersion)
	}
	for _, ext := range exts {
		if !cfg.HasExtension(ext.id) {
			t.Errorf("HasExtension(%s) = false", ext.id)
		}
	}
	if cfg.HasExtension("com.example.other") {
		t.Error("HasExtension(com.example.other) = true")
	}
	var w workctlSettings
	if ok, err := cfg.DecodeExtension("app.cerebrai.workctl", 1, &w); err != nil || !ok || w.SchemaVersion != 1 {
		t.Errorf("workctl settings: %+v, %v, %v", w, ok, err)
	}
	var tool toolSettings
	if ok, err := cfg.DecodeExtension("com.example.tool", 3, &tool); err != nil || !ok || tool != (toolSettings{SchemaVersion: 3}) {
		t.Errorf("tool settings: %+v, %v, %v", tool, ok, err)
	}
}

// The user edits the extension's settings by hand; a newer release of the
// extension reads them, an older one refuses them.
func TestExtensionSettingsAcrossReleases(t *testing.T) {
	path := writeConfig(t, "schema_version: 1\nextensions:\n  com.example.tool:\n    schema_version: 2 # edited\n    color: red\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		SchemaVersion int    `yaml:"schema_version"`
		Color         string `yaml:"color"`
	}
	for _, newest := range []int{2, 3} {
		if ok, err := cfg.DecodeExtension("com.example.tool", newest, &s); err != nil || !ok || s.Color != "red" {
			t.Errorf("release reading up to %d: %+v, %v, %v", newest, s, ok, err)
		}
	}
	if _, err := cfg.DecodeExtension("com.example.tool", 1, &s); err == nil || !strings.Contains(err.Error(), "upgrade") {
		t.Errorf("release reading up to 1: %v", err)
	}
	if _, err := addSettings(path, "com.example.tool", 1); err == nil || !strings.Contains(err.Error(), "upgrade") {
		t.Errorf("AddExtension from release reading up to 1: %v", err)
	}
}

func TestDecodeExtensionTypeMismatch(t *testing.T) {
	cfg, err := Unmarshal([]byte("schema_version: 1\nextensions:\n  com.example.tool: {schema_version: 1, count: many}\n"))
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		SchemaVersion int `yaml:"schema_version"`
		Count         int `yaml:"count"`
	}
	if _, err := cfg.DecodeExtension("com.example.tool", 1, &s); err == nil || !strings.Contains(err.Error(), "invalid settings for extension com.example.tool") {
		t.Errorf("DecodeExtension: %v", err)
	}
}

func TestExtensionIDsEmpty(t *testing.T) {
	for _, content := range []string{brainOnly, "schema_version: 1\nextensions:\n", "schema_version: 1\nextensions: {}\n"} {
		cfg, err := Unmarshal([]byte(content))
		if err != nil {
			t.Fatal(err)
		}
		if ids := cfg.ExtensionIDs(); len(ids) != 0 {
			t.Errorf("%q: ExtensionIDs = %q", content, ids)
		}
	}
}

func TestInitWritesReadableFile(t *testing.T) {
	path := Path(t.TempDir())
	if _, err := Init(path); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil || cfg.SchemaVersion != SchemaVersion || len(cfg.ExtensionIDs()) != 0 {
		t.Errorf("Load: %+v, %v", cfg, err)
	}
}

// Top-level keys this release does not know, from a newer release or the
// user, are ignored on reading and kept on writing.
func TestUnknownTopLevelKeys(t *testing.T) {
	const content = "schema_version: 1\n# notes\nowner: me # the user's own\n" +
		"future_feature:\n  enabled: true\n  list: [1, 2]\nextension_bins: [workctl]\n"
	cfg, err := Unmarshal([]byte(content))
	if err != nil || cfg.SchemaVersion != 1 || len(cfg.ExtensionIDs()) != 0 {
		t.Fatalf("Unmarshal: %+v, %v", cfg, err)
	}
	path := writeConfig(t, content)
	if created, err := Init(path); err != nil || created {
		t.Errorf("Init: %v, %v", created, err)
	}
	if got := readFile(t, path); got != content {
		t.Errorf("Init changed the file to %q", got)
	}
	if _, err := AddExtension(path, "app.cerebrai.workctl", workctlBase); err != nil {
		t.Fatal(err)
	}
	want := content + "extensions:\n  app.cerebrai.workctl:\n    schema_version: 1\n    name: workctl\n    bin: workctl\n"
	if got := readFile(t, path); got != want {
		t.Errorf("file = %q, want %q", got, want)
	}
}
