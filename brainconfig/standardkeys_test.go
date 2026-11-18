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

// The standard keys of an extension's settings: schema_version, name and
// bin.

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

func TestCheckName(t *testing.T) {
	for _, name := range []string{"workctl", "Example Tool", "Ünïcode ✓", "a"} {
		if err := CheckName(name); err != nil {
			t.Errorf("%q: %v", name, err)
		}
	}
	for name, wantErr := range map[string]string{
		"":           "empty",
		"  ":         "empty",
		" workctl":   "leading or trailing space",
		"workctl\t":  "leading or trailing space",
		"two\nlines": "one line",
		"cr\rhere":   "one line",
	} {
		if err := CheckName(name); err == nil || !strings.Contains(err.Error(), "invalid name") || !strings.Contains(err.Error(), wantErr) {
			t.Errorf("%q: %v; want an error containing %q", name, err, wantErr)
		}
	}
}

// settings is the config with one extension, com.example.tool, holding s.
func settings(s string) string {
	return "schema_version: 1\nextensions:\n  com.example.tool: " + s + "\n"
}

func TestExtension(t *testing.T) {
	for in, want := range map[string]ExtensionBase{
		settings("{schema_version: 1}"):                                {SchemaVersion: 1},
		settings("{schema_version: 1, name: Tool, bin: tool}"):         {SchemaVersion: 1, Name: "Tool", Bin: "tool"},
		settings("{schema_version: 1, bin: /opt/x/tool, color: blue}"): {SchemaVersion: 1, Bin: "/opt/x/tool"},
		settings("{name: Tool}"):                                       {Name: "Tool"}, // brainctl lists whatever the version
		settings("{schema_version: 99, name: Tool, bin: tool}"):        {SchemaVersion: 99, Name: "Tool", Bin: "tool"},
		settings("{schema_version: 1, name: 'Tool: the sequel'}"):      {SchemaVersion: 1, Name: "Tool: the sequel"},
	} {
		cfg, err := Unmarshal([]byte(in))
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if got, ok, err := cfg.Extension("com.example.tool"); err != nil || !ok || got != want {
			t.Errorf("%q: Extension = %+v, %v, %v; want %+v", in, got, ok, err, want)
		}
	}
	cfg, err := Unmarshal([]byte(settings("{schema_version: 1}")))
	if err != nil {
		t.Fatal(err)
	}
	if got, ok, err := cfg.Extension("com.example.other"); err != nil || ok || got != (ExtensionBase{}) {
		t.Errorf("missing extension: %+v, %v, %v", got, ok, err)
	}
	for in, wantErr := range map[string]string{
		settings("not a mapping"):                  "must be a mapping",
		settings("{bin: bin/tool}"):                `invalid bin "bin/tool"`,
		settings("{bin: ''}"):                      "invalid bin: empty",
		settings("{bin: null}"):                    "invalid bin: empty",
		settings("{bin: [a, b]}"):                  "invalid settings for extension com.example.tool",
		settings("{name: ''}"):                     "invalid name: empty",
		settings("{name: ' Tool'}"):                "invalid name",
		settings(`{name: "two\nlines"}`):           "one line",
		settings("{name: {a: b}}"):                 "invalid settings for extension com.example.tool",
		settings("{schema_version: x, bin: tool}"): "invalid settings for extension com.example.tool",
	} {
		// Reading the file never looks inside extensions...
		cfg, err := Unmarshal([]byte(in))
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		// ...but asking for the standard keys does.
		if got, ok, err := cfg.Extension("com.example.tool"); err == nil || !ok || !strings.Contains(err.Error(), wantErr) {
			t.Errorf("%q: Extension = %+v, %v, %v; want an error containing %q", in, got, ok, err, wantErr)
		}
	}
}

// DecodeExtension reads the standard keys into a type that embeds
// ExtensionBase and checks them, and ignores keys a type has no field for.
func TestDecodeExtensionStandardKeys(t *testing.T) {
	type withBase struct {
		ExtensionBase `yaml:",inline"`
		Color         string `yaml:"color"`
	}
	cfg, err := Unmarshal([]byte(settings("{schema_version: 2, name: Tool, bin: tool, color: red}")))
	if err != nil {
		t.Fatal(err)
	}
	var s withBase
	if ok, err := cfg.DecodeExtension("com.example.tool", 2, &s); err != nil || !ok ||
		s != (withBase{ExtensionBase{SchemaVersion: 2, Name: "Tool", Bin: "tool"}, "red"}) {
		t.Errorf("DecodeExtension: %+v, %v, %v", s, ok, err)
	}
	// A type without ExtensionBase, or without a field for every key, reads
	// what it has fields for and ignores the rest.
	var without struct {
		Color string `yaml:"color"`
	}
	if ok, err := cfg.DecodeExtension("com.example.tool", 2, &without); err != nil || !ok || without.Color != "red" {
		t.Errorf("DecodeExtension without ExtensionBase: %+v, %v, %v", without, ok, err)
	}
	// A value of the wrong type for its field is still an error.
	var wrongType struct {
		Color int `yaml:"color"`
	}
	if _, err := cfg.DecodeExtension("com.example.tool", 2, &wrongType); err == nil || !strings.Contains(err.Error(), "invalid settings for extension com.example.tool") {
		t.Errorf("DecodeExtension into the wrong type: %v", err)
	}
	for _, bad := range []string{"{schema_version: 1, bin: bin/tool}", "{schema_version: 1, name: ''}"} {
		cfg, err := Unmarshal([]byte(settings(bad)))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := cfg.DecodeExtension("com.example.tool", 1, &s); err == nil || !strings.Contains(err.Error(), "invalid") {
			t.Errorf("%s: DecodeExtension: %v", bad, err)
		}
	}
}

var workctlBase = ExtensionBase{SchemaVersion: 1, Name: "workctl", Bin: "workctl"}

func TestAddExtensionWritesStandardKeys(t *testing.T) {
	for _, tt := range []struct {
		name      string
		in        string
		want      ExtensionBase
		wantAdded []string
		wantFile  string
	}{
		{"all keys", brainOnly, workctlBase, []string{"schema_version", "name", "bin"},
			brainOnly + "extensions:\n  app.cerebrai.workctl:\n    schema_version: 1\n    name: workctl\n    bin: workctl\n"},
		{"no name or bin", brainOnly, ExtensionBase{SchemaVersion: 2}, []string{"schema_version"},
			brainOnly + "extensions:\n  app.cerebrai.workctl:\n    schema_version: 2\n"},
		{"name only", brainOnly, ExtensionBase{SchemaVersion: 1, Name: "Work"}, []string{"schema_version", "name"},
			brainOnly + "extensions:\n  app.cerebrai.workctl:\n    schema_version: 1\n    name: Work\n"},
		{"name that needs quoting", brainOnly, ExtensionBase{SchemaVersion: 1, Name: "Work: the tool"}, []string{"schema_version", "name"},
			brainOnly + "extensions:\n  app.cerebrai.workctl:\n    schema_version: 1\n    name: 'Work: the tool'\n"},
		{"fills in both", "schema_version: 1\nextensions:\n  app.cerebrai.workctl:\n    schema_version: 1 # mine\n    color: blue\n",
			workctlBase, []string{"name", "bin"},
			"schema_version: 1\nextensions:\n  app.cerebrai.workctl:\n    schema_version: 1 # mine\n    color: blue\n    name: workctl\n    bin: workctl\n"},
		{"fills in bin, keeps the user's name", "schema_version: 1\nextensions:\n  app.cerebrai.workctl: {schema_version: 1, name: My Work}\n",
			workctlBase, []string{"bin"},
			"schema_version: 1\nextensions:\n  app.cerebrai.workctl: {schema_version: 1, name: My Work, bin: workctl}\n"},
		{"fills in name, keeps the user's bin", "schema_version: 1\nextensions:\n  app.cerebrai.workctl:\n    schema_version: 1\n    bin: /opt/x/workctl\n",
			workctlBase, []string{"name"},
			"schema_version: 1\nextensions:\n  app.cerebrai.workctl:\n    schema_version: 1\n    bin: /opt/x/workctl\n    name: workctl\n"},
		{"older settings", "schema_version: 1\nextensions:\n  app.cerebrai.workctl: {schema_version: 1}\n",
			ExtensionBase{SchemaVersion: 3, Bin: "workctl"}, []string{"bin"},
			"schema_version: 1\nextensions:\n  app.cerebrai.workctl: {schema_version: 1, bin: workctl}\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfig(t, tt.in)
			added, err := AddExtension(path, "app.cerebrai.workctl", tt.want)
			if err != nil || !slices.Equal(added, tt.wantAdded) {
				t.Fatalf("AddExtension: %q, %v; want %q", added, err, tt.wantAdded)
			}
			if got := readFile(t, path); got != tt.wantFile {
				t.Errorf("file = %q, want %q", got, tt.wantFile)
			}
			// Again: nothing left to add, nothing changes.
			if added, err := AddExtension(path, "app.cerebrai.workctl", tt.want); err != nil || len(added) != 0 {
				t.Errorf("AddExtension again: %q, %v", added, err)
			}
			if got := readFile(t, path); got != tt.wantFile {
				t.Errorf("file changed to %q", got)
			}
		})
	}
}

// A name or bin the user set is never replaced, whatever it is.
func TestAddExtensionKeepsUsersValues(t *testing.T) {
	for _, kv := range []string{"name: workctl", "name: My Work", "bin: workctl", "bin: /opt/tools/bin/workctl", "bin: workctl-dev"} {
		content := "schema_version: 1\nextensions:\n  app.cerebrai.workctl:\n    schema_version: 1\n    name: N\n    bin: b\n"
		key := strings.SplitN(kv, ":", 2)[0]
		content = strings.Replace(content, "    "+key+": "+map[string]string{"name": "N", "bin": "b"}[key], "    "+kv+" # mine", 1)
		path := writeConfig(t, content)
		if added, err := AddExtension(path, "app.cerebrai.workctl", workctlBase); err != nil || len(added) != 0 {
			t.Errorf("%s: AddExtension: %q, %v", kv, added, err)
		}
		if got := readFile(t, path); got != content {
			t.Errorf("%s: file changed to %q", kv, got)
		}
	}
}

func TestAddExtensionRejectsStandardKeys(t *testing.T) {
	const workctlSettings = "schema_version: 1\nextensions:\n  app.cerebrai.workctl: {schema_version: 1}\n"
	for _, tt := range []struct {
		name, content string
		want          ExtensionBase
		wantErr       string
	}{
		{"relative bin", workctlSettings, ExtensionBase{SchemaVersion: 1, Bin: "bin/workctl"}, "invalid bin"},
		{"name with spaces around", workctlSettings, ExtensionBase{SchemaVersion: 1, Name: " workctl"}, "invalid name"},
		{"two-line name", workctlSettings, ExtensionBase{SchemaVersion: 1, Name: "a\nb"}, "invalid name"},
		{"existing bin bad", "schema_version: 1\nextensions:\n  app.cerebrai.workctl: {schema_version: 1, bin: a/b}\n",
			workctlBase, `invalid bin "a/b"`},
		{"existing name bad", "schema_version: 1\nextensions:\n  app.cerebrai.workctl: {schema_version: 1, name: ''}\n",
			workctlBase, "invalid name: empty"},
		{"existing name not text", "schema_version: 1\nextensions:\n  app.cerebrai.workctl: {schema_version: 1, name: [a]}\n",
			workctlBase, "invalid settings for extension app.cerebrai.workctl"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfig(t, tt.content)
			if added, err := AddExtension(path, "app.cerebrai.workctl", tt.want); err == nil || len(added) != 0 || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("AddExtension: %q, %v; want an error containing %q", added, err, tt.wantErr)
			}
			if got := readFile(t, path); got != tt.content {
				t.Errorf("file changed to %q", got)
			}
		})
	}
	missing := Path(t.TempDir())
	if _, err := AddExtension(missing, "app.cerebrai.workctl", workctlBase); !errors.Is(err, ErrNoConfig) {
		t.Errorf("missing file: %v", err)
	}
}

func TestAddExtensionCannotFillIn(t *testing.T) {
	const content = "schema_version: 1\nextensions:\n  app.cerebrai.workctl: {schema_version: 1}\n"
	path := filepath.Join(t.TempDir(), longName)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Skipf("file system does not take a %d byte name: %v", len(longName), err)
	}
	if added, err := AddExtension(path, "app.cerebrai.workctl", workctlBase); err == nil || len(added) != 0 || !strings.Contains(err.Error(), "writing "+path) {
		t.Errorf("AddExtension: %q, %v", added, err)
	}
	if got := readFile(t, path); got != content {
		t.Errorf("file changed to %q", got)
	}
	if names := dirEntries(t, filepath.Dir(path)); len(names) != 1 {
		t.Errorf("left files behind: %q", names)
	}
}

// Extensions' settings with their standard keys, added in turn, as workctl
// and a third-party extension do.
func TestStandardKeysTogether(t *testing.T) {
	path := writeConfig(t, brainOnly)
	for id, base := range map[string]ExtensionBase{
		"app.cerebrai.workctl": workctlBase,
	} {
		if _, err := AddExtension(path, id, base); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := AddExtension(path, "com.example.tool", ExtensionBase{SchemaVersion: 2, Name: "Example Tool", Bin: "/opt/x/bin/tool"}); err != nil {
		t.Fatal(err)
	}
	const want = "schema_version: 1\nextensions:\n" +
		"  app.cerebrai.workctl:\n    schema_version: 1\n    name: workctl\n    bin: workctl\n" +
		"  com.example.tool:\n    schema_version: 2\n    name: Example Tool\n    bin: /opt/x/bin/tool\n"
	if got := readFile(t, path); got != want {
		t.Errorf("file = %q, want %q", got, want)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]ExtensionBase{
		"app.cerebrai.workctl": workctlBase,
		"com.example.tool":     {SchemaVersion: 2, Name: "Example Tool", Bin: "/opt/x/bin/tool"},
	} {
		if got, ok, err := cfg.Extension(id); err != nil || !ok || got != want {
			t.Errorf("Extension(%s) = %+v, %v, %v; want %+v", id, got, ok, err, want)
		}
	}
}

// extensions: *anchor reads as the mapping it refers to. Adding settings
// would replace the alias and drop the other extensions', so it is refused
// and the file left as it is.
func TestAddExtensionRefusesAliasedExtensions(t *testing.T) {
	const content = "schema_version: 1\nshared: &shared\n  com.example.tool: {schema_version: 3, name: Tool}\nextensions: *shared\n"
	path := writeConfig(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if ext, ok, err := cfg.Extension("com.example.tool"); err != nil || !ok || ext.Name != "Tool" {
		t.Fatalf("Extension through the alias: %+v, %v, %v", ext, ok, err)
	}
	added, err := AddExtension(path, "app.cerebrai.workctl", workctlBase)
	if err == nil || len(added) != 0 || !strings.Contains(err.Error(), "extensions refers to another value (a YAML alias)") {
		t.Errorf("AddExtension: %q, %v", added, err)
	}
	if got := readFile(t, path); got != content {
		t.Errorf("file changed to %q", got)
	}
}

// Control characters are refused in name and bin: the file can come from a
// cloned repository, and these reach the terminal.
func TestCheckControlCharacters(t *testing.T) {
	for _, bad := range []string{"a\x1b[2Jb", "a\tb", "a\x00b", "a\u0085b", "a\x7fb"} {
		if err := CheckName(bad); err == nil {
			t.Errorf("CheckName(%q) accepted", bad)
		}
		if err := CheckBin(bad); err == nil {
			t.Errorf("CheckBin(%q) accepted", bad)
		}
		if err := CheckBin("/opt/" + bad); err == nil {
			t.Errorf("CheckBin(/opt/%q) accepted", bad)
		}
	}
	cfg, err := Unmarshal([]byte(settings(`{schema_version: 1, name: "x\e[2Jy"}`)))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := cfg.Extension("com.example.tool"); err == nil || !strings.Contains(err.Error(), "control characters") {
		t.Errorf("Extension with an escape code in name: %v", err)
	}
}
