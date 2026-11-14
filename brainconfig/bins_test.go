package brainconfig

import (
	"errors"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// The extension_bins key: the extension executables brainctl can discover.

func TestCheckBin(t *testing.T) {
	valid := []string{"workctl", "example-tool", "tool.exe", "/opt/tools/bin/tool", "/usr/local/bin/workctl"}
	if runtime.GOOS == "windows" {
		valid = append(valid, `C:\Tools\tool.exe`)
	}
	for _, bin := range valid {
		if err := CheckBin(bin); err != nil {
			t.Errorf("%q: %v", bin, err)
		}
	}
	for _, bin := range []string{
		"", " ", " workctl", "workctl ", "bin/tool", "./tool", "../tool", ".", "..", `bin\tool`, `.\tool`,
	} {
		if err := CheckBin(bin); err == nil || !strings.Contains(err.Error(), "extension_bins") {
			t.Errorf("%q: %v", bin, err)
		}
	}
}

func TestUnmarshalExtensionBins(t *testing.T) {
	for in, want := range map[string][]string{
		brainOnly:                                                                   nil,
		"schema_version: 1\nextension_bins:\n":                                      nil,
		"schema_version: 1\nextension_bins: null\n":                                 nil,
		"schema_version: 1\nextension_bins: []\n":                                   nil,
		"schema_version: 1\nextension_bins: [b, a]\n":                               {"b", "a"},
		"schema_version: 1\nextension_bins:\n  - workctl # ours\n  - /opt/x/tool\n": {"workctl", "/opt/x/tool"},
	} {
		cfg, err := Unmarshal([]byte(in))
		if err != nil {
			t.Errorf("%q: %v", in, err)
			continue
		}
		if got := cfg.ExtensionBins(); !slices.Equal(got, want) {
			t.Errorf("%q: ExtensionBins = %q, want %q", in, got, want)
		}
	}
	for in, wantErr := range map[string]string{
		"schema_version: 1\nextension_bins: workctl\n":            "invalid config file",
		"schema_version: 1\nextension_bins: {a: b}\n":             "invalid config file",
		"schema_version: 1\nextension_bins: [[a]]\n":              "invalid config file",
		"schema_version: 1\nextension_bins: [workctl, workctl]\n": `"workctl" is listed twice`,
		"schema_version: 1\nextension_bins: [bin/tool]\n":         `invalid extension_bins entry "bin/tool"`,
		"schema_version: 1\nextension_bins: ['']\n":               "invalid extension_bins entry: empty",
	} {
		if _, err := Unmarshal([]byte(in)); err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Errorf("%q: got %v, want an error containing %q", in, err, wantErr)
		}
	}
}

func TestExtensionBinsIsACopy(t *testing.T) {
	cfg, err := Unmarshal([]byte("schema_version: 1\nextension_bins: [a, b]\n"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.ExtensionBins()[0] = "changed"
	if got := cfg.ExtensionBins(); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("ExtensionBins = %q after changing a returned slice", got)
	}
}

func TestAddExtensionBin(t *testing.T) {
	for _, tt := range []struct{ name, in, want string }{
		{"no key", brainOnly, brainOnly + "extension_bins:\n  - workctl\n"},
		{"key without value", "schema_version: 1\nextension_bins:\n", brainOnly + "extension_bins:\n  - workctl\n"},
		{"null", "schema_version: 1\nextension_bins: null\n", brainOnly + "extension_bins:\n  - workctl\n"},
		{"empty flow list", "schema_version: 1\nextension_bins: []\n", brainOnly + "extension_bins:\n  - workctl\n"},
		{"other entries and comments",
			"# mine\nschema_version: 1\nextension_bins:\n  - /opt/x/tool # theirs\n",
			"# mine\nschema_version: 1\nextension_bins:\n  - /opt/x/tool # theirs\n  - workctl\n"},
		{"after extensions",
			"schema_version: 1\nextensions:\n  app.cerebrai.workctl:\n    schema_version: 1\n",
			"schema_version: 1\nextensions:\n  app.cerebrai.workctl:\n    schema_version: 1\nextension_bins:\n  - workctl\n"},
		{"before extensions",
			"schema_version: 1\nextension_bins: [tool]\nextensions: {}\n",
			"schema_version: 1\nextension_bins:\n  - tool\n  - workctl\nextensions: {}\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfig(t, tt.in)
			if added, err := AddExtensionBin(path, "workctl"); err != nil || !added {
				t.Fatalf("AddExtensionBin: %v, %v", added, err)
			}
			if got := readFile(t, path); got != tt.want {
				t.Errorf("file = %q, want %q", got, tt.want)
			}
			// Again: already listed, nothing changes.
			if added, err := AddExtensionBin(path, "workctl"); err != nil || added {
				t.Errorf("AddExtensionBin again: %v, %v", added, err)
			}
			if got := readFile(t, path); got != tt.want {
				t.Errorf("file changed to %q", got)
			}
		})
	}
}

func TestAddExtensionBinAbsolutePath(t *testing.T) {
	path := writeConfig(t, brainOnly)
	if added, err := AddExtensionBin(path, "/opt/x/bin/tool"); err != nil || !added {
		t.Fatalf("AddExtensionBin: %v, %v", added, err)
	}
	cfg, err := Load(path)
	if err != nil || !slices.Equal(cfg.ExtensionBins(), []string{"/opt/x/bin/tool"}) {
		t.Errorf("Load: %q, %v", cfg.ExtensionBins(), err)
	}
}

func TestAddExtensionBinRejects(t *testing.T) {
	for _, tt := range []struct{ name, content, bin, wantErr string }{
		{"relative path", brainOnly, "bin/tool", "invalid extension_bins entry"},
		{"empty", brainOnly, "", "invalid extension_bins entry"},
		{"bad file", "schema_version: 2\n", "workctl", "upgrade"},
		{"bad list", "schema_version: 1\nextension_bins: [a, a]\n", "workctl", "listed twice"},
		{"list not a list", "schema_version: 1\nextension_bins: a\n", "workctl", "invalid config file"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfig(t, tt.content)
			if added, err := AddExtensionBin(path, tt.bin); err == nil || added || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("AddExtensionBin: %v, %v; want an error containing %q", added, err, tt.wantErr)
			}
			if got := readFile(t, path); got != tt.content {
				t.Errorf("file changed to %q", got)
			}
		})
	}
	missing := Path(t.TempDir())
	if _, err := AddExtensionBin(missing, "workctl"); !errors.Is(err, ErrNoConfig) {
		t.Errorf("missing file: %v", err)
	}
}

func TestAddExtensionBinCannotWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, longName)
	writeLong(t, path)
	if added, err := AddExtensionBin(path, "workctl"); err == nil || added || !strings.Contains(err.Error(), "writing "+path) {
		t.Errorf("AddExtensionBin: %v, %v", added, err)
	}
	if got := readFile(t, path); got != brainOnly {
		t.Errorf("file changed to %q", got)
	}
}

// Extensions' settings and their entries in extension_bins live side by side,
// and adding one never disturbs the other.
func TestSettingsAndBinsTogether(t *testing.T) {
	path := writeConfig(t, brainOnly)
	steps := []func() (bool, error){
		func() (bool, error) { return AddExtension(path, "app.cerebrai.workctl", 1) },
		func() (bool, error) { return AddExtensionBin(path, "workctl") },
		func() (bool, error) { return AddExtensionBin(path, "/opt/x/bin/tool") },
		func() (bool, error) { return AddExtension(path, "com.example.tool", 2) },
	}
	for i, step := range steps {
		if added, err := step(); err != nil || !added {
			t.Fatalf("step %d: %v, %v", i, added, err)
		}
	}
	const want = "schema_version: 1\nextensions:\n" +
		"  app.cerebrai.workctl:\n    schema_version: 1\n" +
		"  com.example.tool:\n    schema_version: 2\n" +
		"extension_bins:\n  - workctl\n  - /opt/x/bin/tool\n"
	if got := readFile(t, path); got != want {
		t.Errorf("file = %q, want %q", got, want)
	}
	if _, err := Init(path); err != nil {
		t.Errorf("Init on the result: %v", err)
	}
}
