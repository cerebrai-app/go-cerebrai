package brainconfig

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// An extension's bin: the executable brainctl can discover, kept next to the
// extension's schema_version in its settings.

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
		if err := CheckBin(bin); err == nil || !strings.Contains(err.Error(), "invalid bin") {
			t.Errorf("%q: %v", bin, err)
		}
	}
}

// settings is the config with one extension, com.example.tool, holding s.
func settings(s string) string {
	return "schema_version: 1\nextensions:\n  com.example.tool: " + s + "\n"
}

func TestExtensionBin(t *testing.T) {
	for in, want := range map[string]string{
		brainOnly:                       "",
		settings("{schema_version: 1}"): "",
		settings("{schema_version: 1, bin: tool}"):                        "tool",
		settings("{schema_version: 1, bin: /opt/x/tool, color: blue}"):    "/opt/x/tool",
		settings("{bin: tool}"):                                           "tool", // brainctl lists whatever the version
		settings("{schema_version: 99, bin: tool}"):                       "tool",
		"schema_version: 1\nextensions:\n  com.example.other: {bin: x}\n": "",
	} {
		cfg, err := Unmarshal([]byte(in))
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if got, err := cfg.ExtensionBin("com.example.tool"); err != nil || got != want {
			t.Errorf("%q: ExtensionBin = %q, %v; want %q", in, got, err, want)
		}
	}
	for in, wantErr := range map[string]string{
		settings("not a mapping"):                  "must be a mapping",
		settings("{bin: bin/tool}"):                `invalid bin "bin/tool"`,
		settings("{bin: ''}"):                      "invalid bin: empty",
		settings("{bin: null}"):                    "invalid bin: empty",
		settings("{bin: [a, b]}"):                  "invalid settings for extension com.example.tool",
		settings("{schema_version: x, bin: tool}"): "invalid settings for extension com.example.tool",
	} {
		// Reading the file never looks inside extensions...
		cfg, err := Unmarshal([]byte(in))
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		// ...but asking for the bin does.
		if got, err := cfg.ExtensionBin("com.example.tool"); err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Errorf("%q: ExtensionBin = %q, %v; want an error containing %q", in, got, err, wantErr)
		}
	}
}

// DecodeExtension accepts bin in a type that embeds ExtensionBase, checks it,
// and rejects it in a type that does not.
func TestDecodeExtensionBin(t *testing.T) {
	type withBase struct {
		ExtensionBase `yaml:",inline"`
		Color         string `yaml:"color"`
	}
	cfg, err := Unmarshal([]byte(settings("{schema_version: 2, bin: tool, color: red}")))
	if err != nil {
		t.Fatal(err)
	}
	var s withBase
	if ok, err := cfg.DecodeExtension("com.example.tool", 2, &s); err != nil || !ok ||
		s != (withBase{ExtensionBase{SchemaVersion: 2, Bin: "tool"}, "red"}) {
		t.Errorf("DecodeExtension: %+v, %v, %v", s, ok, err)
	}
	var without struct {
		SchemaVersion int    `yaml:"schema_version"`
		Color         string `yaml:"color"`
	}
	if _, err := cfg.DecodeExtension("com.example.tool", 2, &without); err == nil || !strings.Contains(err.Error(), "bin") {
		t.Errorf("DecodeExtension without ExtensionBase: %v", err)
	}
	cfg, err = Unmarshal([]byte(settings("{schema_version: 1, bin: bin/tool}")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.DecodeExtension("com.example.tool", 1, &s); err == nil || !strings.Contains(err.Error(), "invalid bin") {
		t.Errorf("DecodeExtension with a bad bin: %v", err)
	}
}

func TestAddExtensionBin(t *testing.T) {
	for _, tt := range []struct{ name, in, want string }{
		{"block settings",
			"schema_version: 1\nextensions:\n  app.cerebrai.workctl:\n    schema_version: 1\n",
			"schema_version: 1\nextensions:\n  app.cerebrai.workctl:\n    schema_version: 1\n    bin: workctl\n"},
		{"flow settings",
			"schema_version: 1\nextensions:\n  app.cerebrai.workctl: {schema_version: 1}\n",
			"schema_version: 1\nextensions:\n  app.cerebrai.workctl: {schema_version: 1, bin: workctl}\n"},
		{"comments and other settings kept",
			"# mine\nschema_version: 1\nextensions:\n  app.cerebrai.workctl:\n    schema_version: 1 # format\n    color: blue\n" +
				"  com.example.tool:\n    schema_version: 3\n    bin: tool\n",
			"# mine\nschema_version: 1\nextensions:\n  app.cerebrai.workctl:\n    schema_version: 1 # format\n    color: blue\n    bin: workctl\n" +
				"  com.example.tool:\n    schema_version: 3\n    bin: tool\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfig(t, tt.in)
			if added, err := AddExtensionBin(path, "app.cerebrai.workctl", "workctl"); err != nil || !added {
				t.Fatalf("AddExtensionBin: %v, %v", added, err)
			}
			if got := readFile(t, path); got != tt.want {
				t.Errorf("file = %q, want %q", got, tt.want)
			}
			// Again: already set, nothing changes.
			if added, err := AddExtensionBin(path, "app.cerebrai.workctl", "workctl"); err != nil || added {
				t.Errorf("AddExtensionBin again: %v, %v", added, err)
			}
			if got := readFile(t, path); got != tt.want {
				t.Errorf("file changed to %q", got)
			}
		})
	}
}

// A bin the user set is never replaced, whatever it is.
func TestAddExtensionBinKeepsUsersBin(t *testing.T) {
	for _, bin := range []string{"workctl", "/opt/tools/bin/workctl", "other-name"} {
		content := "schema_version: 1\nextensions:\n  app.cerebrai.workctl:\n    schema_version: 1\n    bin: " + bin + " # mine\n"
		path := writeConfig(t, content)
		if added, err := AddExtensionBin(path, "app.cerebrai.workctl", "workctl"); err != nil || added {
			t.Errorf("%s: AddExtensionBin: %v, %v", bin, added, err)
		}
		if got := readFile(t, path); got != content {
			t.Errorf("%s: file changed to %q", bin, got)
		}
	}
}

func TestAddExtensionBinRejects(t *testing.T) {
	const workctlSettings = "schema_version: 1\nextensions:\n  app.cerebrai.workctl: {schema_version: 1}\n"
	for _, tt := range []struct{ name, content, id, bin, wantErr string }{
		{"relative path", workctlSettings, "app.cerebrai.workctl", "bin/workctl", "invalid bin"},
		{"empty", workctlSettings, "app.cerebrai.workctl", "", "invalid bin: empty"},
		{"bad ID", workctlSettings, "workctl", "workctl", "invalid extension ID"},
		{"no settings", brainOnly, "app.cerebrai.workctl", "workctl", "no settings for extension app.cerebrai.workctl"},
		{"settings not a mapping", "schema_version: 1\nextensions:\n  app.cerebrai.workctl: 1\n",
			"app.cerebrai.workctl", "workctl", "must be a mapping"},
		{"existing bin bad", "schema_version: 1\nextensions:\n  app.cerebrai.workctl: {schema_version: 1, bin: a/b}\n",
			"app.cerebrai.workctl", "workctl", `invalid bin "a/b"`},
		{"bad file", "schema_version: 2\n", "app.cerebrai.workctl", "workctl", "upgrade"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfig(t, tt.content)
			if added, err := AddExtensionBin(path, tt.id, tt.bin); err == nil || added || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("AddExtensionBin: %v, %v; want an error containing %q", added, err, tt.wantErr)
			}
			if got := readFile(t, path); got != tt.content {
				t.Errorf("file changed to %q", got)
			}
		})
	}
	missing := Path(t.TempDir())
	if _, err := AddExtensionBin(missing, "app.cerebrai.workctl", "workctl"); !errors.Is(err, ErrNoConfig) {
		t.Errorf("missing file: %v", err)
	}
}

// AddExtension refuses existing settings with a bad bin, as it does a bad
// schema_version.
func TestAddExtensionChecksExistingBin(t *testing.T) {
	const content = "schema_version: 1\nextensions:\n  app.cerebrai.workctl: {schema_version: 1, bin: a/b}\n"
	path := writeConfig(t, content)
	if added, err := AddExtension(path, "app.cerebrai.workctl", 1); err == nil || added || !strings.Contains(err.Error(), "invalid bin") {
		t.Errorf("AddExtension: %v, %v", added, err)
	}
	if got := readFile(t, path); got != content {
		t.Errorf("file changed to %q", got)
	}
}

func TestAddExtensionBinCannotWrite(t *testing.T) {
	const content = "schema_version: 1\nextensions:\n  app.cerebrai.workctl: {schema_version: 1}\n"
	path := filepath.Join(t.TempDir(), longName)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Skipf("file system does not take a %d byte name: %v", len(longName), err)
	}
	if added, err := AddExtensionBin(path, "app.cerebrai.workctl", "workctl"); err == nil || added || !strings.Contains(err.Error(), "writing "+path) {
		t.Errorf("AddExtensionBin: %v, %v", added, err)
	}
	if got := readFile(t, path); got != content {
		t.Errorf("file changed to %q", got)
	}
	if names := dirEntries(t, filepath.Dir(path)); len(names) != 1 {
		t.Errorf("left files behind: %q", names)
	}
}

// Extensions' settings with their bins, added in turn, as workctl and a
// third-party extension do.
func TestSettingsAndBinsTogether(t *testing.T) {
	path := writeConfig(t, brainOnly)
	steps := []func() (bool, error){
		func() (bool, error) { return AddExtension(path, "app.cerebrai.workctl", 1) },
		func() (bool, error) { return AddExtensionBin(path, "app.cerebrai.workctl", "workctl") },
		func() (bool, error) { return AddExtension(path, "com.example.tool", 2) },
		func() (bool, error) { return AddExtensionBin(path, "com.example.tool", "/opt/x/bin/tool") },
	}
	for i, step := range steps {
		if added, err := step(); err != nil || !added {
			t.Fatalf("step %d: %v, %v", i, added, err)
		}
	}
	const want = "schema_version: 1\nextensions:\n" +
		"  app.cerebrai.workctl:\n    schema_version: 1\n    bin: workctl\n" +
		"  com.example.tool:\n    schema_version: 2\n    bin: /opt/x/bin/tool\n"
	if got := readFile(t, path); got != want {
		t.Errorf("file = %q, want %q", got, want)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	for id, bin := range map[string]string{"app.cerebrai.workctl": "workctl", "com.example.tool": "/opt/x/bin/tool"} {
		if got, err := cfg.ExtensionBin(id); err != nil || got != bin {
			t.Errorf("ExtensionBin(%s) = %q, %v; want %q", id, got, err, bin)
		}
	}
}
