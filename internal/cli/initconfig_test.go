package cli

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cerebrai-app/go-cerebrai/brainconfig"
	"github.com/cerebrai-app/go-cerebrai/internal/python"
)

// The config file scenarios of init, end to end through Main: brainctl
// creates .brain/config.yaml, and extensions (workctl and third-party
// programs) add their settings to it under their reverse domain ID.

// configPath is the config file, relative to the test's current directory.
var configPath = filepath.Join(".brain", "config.yaml")

const (
	brainOnly   = "schema_version: 1\n"
	withWorkctl = "schema_version: 1\nextensions:\n  app.cerebrai.workctl:\n    schema_version: 1\n    bin: workctl\n"
)

// project runs the test in an empty current directory with install stubbed
// out, and returns a pointer to the number of times install has run.
func project(t *testing.T) *int {
	t.Helper()
	stubInstall(t, nil)
	calls := 0
	install = func(python.Options) (string, error) {
		// The config file is always settled before the download starts, in
		// the brain home the program used.
		dir, err := brainconfig.Home()
		if err != nil {
			t.Errorf("install ran without a brain home: %v", err)
		} else if _, err := os.Stat(brainconfig.Path(dir)); err != nil {
			t.Errorf("install ran without a config file: %v", err)
		}
		calls++
		return "", nil
	}
	return &calls
}

// writeBrainConfig writes content to the config file, creating .brain.
func writeBrainConfig(t *testing.T, content string) {
	t.Helper()
	if err := os.MkdirAll(".brain", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// brainConfig is the config file's content, or "<missing>".
func brainConfig(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		return "<missing>"
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// runProgram runs prog with args and returns its exit code and stderr.
func runProgram(prog Program, args ...string) (int, string) {
	var errOut strings.Builder
	code := Main(prog, args, io.Discard, &errOut)
	return code, errOut.String()
}

var thirdParty = Program{Name: "tool", ExtensionID: "com.example.tool", ExtensionSchemaVersion: 3}

func TestBrainctlInitCreatesConfig(t *testing.T) {
	installs := project(t)
	code, stderr := runProgram(brainctl, "init")
	if code != 0 || !strings.Contains(stderr, "Wrote "+filepath.Join(mustGetwd(t), configPath)) {
		t.Fatalf("got %d, %q", code, stderr)
	}
	if got := brainConfig(t); got != brainOnly {
		t.Errorf("config = %q, want %q", got, brainOnly)
	}
	if *installs != 1 {
		t.Errorf("install ran %d times, want 1", *installs)
	}
}

func TestBrainctlInitKeepsExistingConfig(t *testing.T) {
	for _, content := range []string{
		brainOnly,
		"# my notes\nschema_version: 1 # format\n\nextensions: {}\n",
		withWorkctl,
	} {
		t.Run("", func(t *testing.T) {
			installs := project(t)
			writeBrainConfig(t, content)
			code, stderr := runProgram(brainctl, "init")
			if code != 0 || !strings.Contains(stderr, "Using existing ") {
				t.Fatalf("got %d, %q", code, stderr)
			}
			if got := brainConfig(t); got != content {
				t.Errorf("config changed to %q", got)
			}
			if *installs != 1 {
				t.Errorf("install ran %d times, want 1", *installs)
			}
		})
	}
}

func TestBrainctlInitStopsOnBadConfig(t *testing.T) {
	for _, tt := range []struct{ name, content, wantErr string }{
		{"empty", "", "empty"},
		{"not YAML", "schema_version: [\n", "invalid config file"},
		{"not a mapping", "- 1\n", "invalid config file"},
		{"two documents", "schema_version: 1\n---\nschema_version: 1\n", "single YAML document"},
		{"no schema version", "extensions: {}\n", "no schema_version"},
		{"negative schema version", "schema_version: -1\n", "invalid schema_version -1"},
		{"newer schema version", "schema_version: 2\n", "upgrade to a newer release"},
		{"unknown top-level key", "schema_version: 1\ncolor: blue\n", "invalid config file"},
		{"extensions not a mapping", "schema_version: 1\nextensions: [a]\n", "invalid config file"},
		{"no top-level extension_bins", "schema_version: 1\nextension_bins: [workctl]\n", "invalid config file"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			installs := project(t)
			writeBrainConfig(t, tt.content)
			code, stderr := runProgram(brainctl, "init")
			if code != 1 || !strings.HasPrefix(stderr, "error: ") || !strings.Contains(stderr, tt.wantErr) {
				t.Errorf("got %d, %q; want an error containing %q", code, stderr, tt.wantErr)
			}
			if got := brainConfig(t); got != tt.content {
				t.Errorf("config changed to %q", got)
			}
			if *installs != 0 {
				t.Error("install ran after a config error")
			}
		})
	}
}

func TestBrainctlInitFailsWhenConfigDirIsAFile(t *testing.T) {
	installs := project(t)
	if err := os.WriteFile(".brain", nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if code, stderr := runProgram(brainctl, "init"); code != 1 || !strings.HasPrefix(stderr, "error: ") {
		t.Errorf("got %d, %q", code, stderr)
	}
	if *installs != 0 {
		t.Error("install ran after a config error")
	}
}

// brainctl never looks inside extensions, so no extension's settings, however
// broken, unknown or new, can stop it.
func TestBrainctlInitIgnoresExtensionSettings(t *testing.T) {
	for _, content := range []string{
		"schema_version: 1\nextensions:\n  com.example.tool: not a mapping\n",
		"schema_version: 1\nextensions:\n  com.example.tool:\n",
		"schema_version: 1\nextensions:\n  com.example.tool: {}\n",
		"schema_version: 1\nextensions:\n  com.example.tool: {schema_version: 99, anything: [1, 2]}\n",
		"schema_version: 1\nextensions:\n  not a reverse domain ID: {schema_version: x}\n",
		"schema_version: 1\nextensions:\n  app.cerebrai.workctl: {schema_version: 1, unknown: true}\n",
	} {
		t.Run("", func(t *testing.T) {
			installs := project(t)
			writeBrainConfig(t, content)
			if code, stderr := runProgram(brainctl, "init"); code != 0 {
				t.Errorf("got %d, %q", code, stderr)
			}
			if got := brainConfig(t); got != content {
				t.Errorf("config changed to %q", got)
			}
			if *installs != 1 {
				t.Errorf("install ran %d times, want 1", *installs)
			}
		})
	}
}

func TestWorkctlInitAfterBrainctlInit(t *testing.T) {
	installs := project(t)
	if code, stderr := runProgram(brainctl, "init"); code != 0 {
		t.Fatalf("brainctl init: got %d, %q", code, stderr)
	}
	code, stderr := runProgram(workctl, "init")
	if code != 0 || !strings.Contains(stderr, "Added settings for extension app.cerebrai.workctl to ") {
		t.Fatalf("workctl init: got %d, %q", code, stderr)
	}
	if strings.Contains(stderr, "Wrote ") {
		t.Errorf("workctl init wrote a new file: %q", stderr)
	}
	if got := brainConfig(t); got != withWorkctl {
		t.Errorf("config = %q, want %q", got, withWorkctl)
	}
	if *installs != 2 {
		t.Errorf("install ran %d times, want 2", *installs)
	}
	for _, dir := range []string{".work", ".workctl"} {
		if _, err := os.Stat(dir); err == nil {
			t.Errorf("workctl init made %s", dir)
		}
	}
}

func TestInitIsIdempotent(t *testing.T) {
	project(t)
	writeBrainConfig(t, withWorkctl)
	for _, prog := range []Program{workctl, brainctl, workctl} {
		code, stderr := runProgram(prog, "init")
		if code != 0 {
			t.Fatalf("%s init: got %d, %q", prog.Name, code, stderr)
		}
		if prog.ExtensionID != "" && !strings.Contains(stderr, "Using existing settings for extension "+prog.ExtensionID) {
			t.Errorf("%s init: %q", prog.Name, stderr)
		}
		if got := brainConfig(t); got != withWorkctl {
			t.Fatalf("%s init changed the config to %q", prog.Name, got)
		}
	}
}

func TestWorkctlInitWithoutConfigFails(t *testing.T) {
	for _, tt := range []struct {
		name  string
		setup func(t *testing.T)
	}{
		{"no .brain", func(*testing.T) {}},
		{"empty .brain", func(t *testing.T) {
			if err := os.Mkdir(".brain", 0o755); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			installs := project(t)
			tt.setup(t)
			code, stderr := runProgram(workctl, "init")
			if code != 1 || !strings.Contains(stderr, "run brainctl init first") {
				t.Errorf("got %d, %q", code, stderr)
			}
			if got := brainConfig(t); got != "<missing>" {
				t.Errorf("workctl init wrote %q", got)
			}
			if *installs != 0 {
				t.Error("install ran without a config file")
			}
		})
	}
	// No .brain home is left behind either.
	project(t)
	runProgram(workctl, "init")
	if _, err := os.Stat(".brain"); err == nil {
		t.Error("workctl init made .brain")
	}
}

func TestWorkctlInitKeepsCommentsAndOtherContent(t *testing.T) {
	project(t)
	writeBrainConfig(t, "# my brain\nschema_version: 1 # format\nextensions:\n"+
		"  com.example.tool:\n    schema_version: 3 # theirs\n    color: blue\n")
	if code, stderr := runProgram(workctl, "init"); code != 0 {
		t.Fatalf("got %d, %q", code, stderr)
	}
	const want = "# my brain\nschema_version: 1 # format\nextensions:\n" +
		"  com.example.tool:\n    schema_version: 3 # theirs\n    color: blue\n" +
		"  app.cerebrai.workctl:\n    schema_version: 1\n    bin: workctl\n"
	if got := brainConfig(t); got != want {
		t.Errorf("config = %q, want %q", got, want)
	}
}

func TestWorkctlInitFillsEmptyExtensions(t *testing.T) {
	for _, content := range []string{
		"schema_version: 1\nextensions:\n",
		"schema_version: 1\nextensions: null\n",
		"schema_version: 1\nextensions: {}\n",
	} {
		t.Run("", func(t *testing.T) {
			project(t)
			writeBrainConfig(t, content)
			if code, stderr := runProgram(workctl, "init"); code != 0 {
				t.Fatalf("got %d, %q", code, stderr)
			}
			if got := brainConfig(t); got != withWorkctl {
				t.Errorf("config = %q, want %q", got, withWorkctl)
			}
		})
	}
}

func TestWorkctlInitStopsOnBadConfig(t *testing.T) {
	for _, tt := range []struct{ name, content, wantErr string }{
		// The file's top level, as brainctl checks it.
		{"empty", "", "empty"},
		{"not YAML", "schema_version: [\n", "invalid config file"},
		{"newer file schema version", "schema_version: 2\n", "upgrade to a newer release"},
		{"unknown top-level key", "schema_version: 1\ncolor: blue\n", "invalid config file"},
		{"settings with bad bin", "schema_version: 1\nextensions:\n  app.cerebrai.workctl: {schema_version: 1, bin: a/b}\n",
			`invalid bin "a/b"`},
		{"settings with empty bin", "schema_version: 1\nextensions:\n  app.cerebrai.workctl: {schema_version: 1, bin: ''}\n",
			"invalid bin: empty"},
		// workctl's own settings, which only workctl checks.
		{"settings not a mapping", "schema_version: 1\nextensions:\n  app.cerebrai.workctl: 1\n",
			"settings for extension app.cerebrai.workctl must be a mapping"},
		{"settings null", "schema_version: 1\nextensions:\n  app.cerebrai.workctl:\n",
			"settings for extension app.cerebrai.workctl must be a mapping"},
		{"settings without schema version", "schema_version: 1\nextensions:\n  app.cerebrai.workctl: {}\n",
			"no schema_version for extension app.cerebrai.workctl"},
		{"settings schema version not a number", "schema_version: 1\nextensions:\n  app.cerebrai.workctl: {schema_version: one}\n",
			"invalid settings for extension app.cerebrai.workctl"},
		{"settings with negative schema version", "schema_version: 1\nextensions:\n  app.cerebrai.workctl: {schema_version: -1}\n",
			"invalid schema_version -1 for extension app.cerebrai.workctl"},
		{"settings with newer schema version", "schema_version: 1\nextensions:\n  app.cerebrai.workctl: {schema_version: 2}\n",
			"schema_version 2 for extension app.cerebrai.workctl, but this release reads only up to 1"},
		{"settings with unknown key", "schema_version: 1\nextensions:\n  app.cerebrai.workctl: {schema_version: 1, color: blue}\n",
			"invalid settings for extension app.cerebrai.workctl"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			installs := project(t)
			writeBrainConfig(t, tt.content)
			code, stderr := runProgram(workctl, "init")
			if code != 1 || !strings.HasPrefix(stderr, "error: ") || !strings.Contains(stderr, tt.wantErr) {
				t.Errorf("got %d, %q; want an error containing %q", code, stderr, tt.wantErr)
			}
			if got := brainConfig(t); got != tt.content {
				t.Errorf("config changed to %q", got)
			}
			if *installs != 0 {
				t.Error("install ran after a config error")
			}
		})
	}
}

// Settings written by an older workctl keep working after an upgrade: only
// versions newer than the running workctl reads fail.
func TestWorkctlInitAcceptsOlderSettings(t *testing.T) {
	project(t)
	newer := workctl
	newer.ExtensionSchemaVersion = 2
	writeBrainConfig(t, withWorkctl)
	if code, stderr := runProgram(newer, "init"); code != 0 || !strings.Contains(stderr, "Using existing settings") {
		t.Errorf("got %d, %q", code, stderr)
	}
	if got := brainConfig(t); got != withWorkctl {
		t.Errorf("config changed to %q", got)
	}
}

func TestThirdPartyExtensionAlongsideWorkctl(t *testing.T) {
	for _, order := range [][]Program{{workctl, thirdParty}, {thirdParty, workctl}} {
		t.Run(order[0].Name+" first", func(t *testing.T) {
			project(t)
			if code, stderr := runProgram(brainctl, "init"); code != 0 {
				t.Fatalf("brainctl init: got %d, %q", code, stderr)
			}
			want := brainOnly + "extensions:\n"
			for _, prog := range order {
				if code, stderr := runProgram(prog, "init"); code != 0 {
					t.Fatalf("%s init: got %d, %q", prog.Name, code, stderr)
				}
				want += "  " + prog.ExtensionID + ":\n    schema_version: " +
					map[string]string{"app.cerebrai.workctl": "1", "com.example.tool": "3"}[prog.ExtensionID] + "\n" +
					"    bin: " + prog.Name + "\n"
			}
			if got := brainConfig(t); got != want {
				t.Errorf("config = %q, want %q", got, want)
			}
			// Every program, run again, leaves the file alone.
			for _, prog := range []Program{brainctl, workctl, thirdParty} {
				if code, stderr := runProgram(prog, "init"); code != 0 {
					t.Errorf("%s init again: got %d, %q", prog.Name, code, stderr)
				}
			}
			if got := brainConfig(t); got != want {
				t.Errorf("config changed to %q", got)
			}
		})
	}
}

// A third-party extension's settings that workctl could not read do not
// stop workctl: each extension checks only its own.
func TestWorkctlInitIgnoresOtherExtensions(t *testing.T) {
	project(t)
	const before = "schema_version: 1\nextensions:\n  com.example.tool: {schema_version: 99, x: [1]}\n  junk: 1\n"
	writeBrainConfig(t, before)
	if code, stderr := runProgram(workctl, "init"); code != 0 {
		t.Fatalf("got %d, %q", code, stderr)
	}
	const want = "schema_version: 1\nextensions:\n  com.example.tool: {schema_version: 99, x: [1]}\n  junk: 1\n" +
		"  app.cerebrai.workctl:\n    schema_version: 1\n    bin: workctl\n"
	if got := brainConfig(t); got != want {
		t.Errorf("config = %q, want %q", got, want)
	}
}

func TestExtensionInitRejectsBadProgram(t *testing.T) {
	for _, tt := range []struct {
		name    string
		prog    Program
		wantErr string
	}{
		{"plain name as ID", Program{Name: "tool", ExtensionID: "tool", ExtensionSchemaVersion: 1}, "invalid extension ID"},
		{"two-label ID", Program{Name: "tool", ExtensionID: "example.tool", ExtensionSchemaVersion: 1}, "invalid extension ID"},
		{"upper case ID", Program{Name: "tool", ExtensionID: "com.Example.tool", ExtensionSchemaVersion: 1}, "invalid extension ID"},
		{"no schema version", Program{Name: "tool", ExtensionID: "com.example.tool"}, "invalid schema version 0"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			installs := project(t)
			writeBrainConfig(t, brainOnly)
			code, stderr := runProgram(tt.prog, "init")
			if code != 1 || !strings.Contains(stderr, tt.wantErr) {
				t.Errorf("got %d, %q; want an error containing %q", code, stderr, tt.wantErr)
			}
			if got := brainConfig(t); got != brainOnly {
				t.Errorf("config changed to %q", got)
			}
			if *installs != 0 {
				t.Error("install ran after a config error")
			}
		})
	}
}

// A failed download leaves the config file in place, so running init again
// only retries the download.
func TestInstallErrorKeepsConfig(t *testing.T) {
	stubInstall(t, errors.New("no download"))
	if code, stderr := runProgram(brainctl, "init"); code != 1 || !strings.HasSuffix(stderr, "error: no download\n") {
		t.Fatalf("brainctl init: got %d, %q", code, stderr)
	}
	if code, stderr := runProgram(workctl, "init"); code != 1 || !strings.HasSuffix(stderr, "error: no download\n") {
		t.Fatalf("workctl init: got %d, %q", code, stderr)
	}
	if got := brainConfig(t); got != withWorkctl {
		t.Errorf("config = %q, want %q", got, withWorkctl)
	}
}

func TestInitUsageDescribesConfig(t *testing.T) {
	for _, tt := range []struct {
		prog Program
		want []string
	}{
		{brainctl, []string{"usage: brainctl init", "Writes .brain/config.yaml"}},
		{workctl, []string{"usage: workctl init", "app.cerebrai.workctl", ".brain/config.yaml", "brainctl init"}},
	} {
		code, stderr := runProgram(tt.prog, "init", "-h")
		if code != 0 {
			t.Errorf("%s: got %d", tt.prog.Name, code)
		}
		for _, want := range tt.want {
			if !strings.Contains(filepath.ToSlash(stderr), want) {
				t.Errorf("%s init -h does not mention %q:\n%s", tt.prog.Name, want, stderr)
			}
		}
	}
	for _, prog := range []Program{brainctl, workctl} {
		if _, stderr := runProgram(prog, "help"); !strings.Contains(filepath.ToSlash(stderr), ".brain/config.yaml") {
			t.Errorf("%s help does not mention the config file:\n%s", prog.Name, stderr)
		}
	}
}

func mustGetwd(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// brainAt makes a brain home, points BRAIN_HOME at it, moves the test to
// a different, empty current directory, and returns the brain home.
func brainAt(t *testing.T) string {
	t.Helper()
	brain := t.TempDir()
	t.Setenv(brainconfig.EnvVar, brain)
	t.Chdir(t.TempDir())
	return brain
}

// cwdIsEmpty fails the test if init wrote anything in the current directory.
func cwdIsEmpty(t *testing.T) {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("wrote %d entries in the current directory, e.g. %s", len(entries), entries[0].Name())
	}
}

func TestBrainHomeFromEnvironment(t *testing.T) {
	installs := project(t)
	brain := brainAt(t)
	code, stderr := runProgram(brainctl, "init")
	path := filepath.Join(brain, ".brain", "config.yaml")
	if code != 0 || !strings.Contains(stderr, "Wrote "+path) {
		t.Fatalf("brainctl init: got %d, %q", code, stderr)
	}
	if code, stderr := runProgram(workctl, "init"); code != 0 || !strings.Contains(stderr, "Added settings for extension app.cerebrai.workctl to "+path) {
		t.Fatalf("workctl init: got %d, %q", code, stderr)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != withWorkctl {
		t.Errorf("config = %q, %v; want %q", data, err, withWorkctl)
	}
	cwdIsEmpty(t)
	if *installs != 2 {
		t.Errorf("install ran %d times, want 2", *installs)
	}
}

func TestBrainHomeRelative(t *testing.T) {
	project(t)
	if err := os.Mkdir("notes", 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(brainconfig.EnvVar, "notes")
	if code, stderr := runProgram(brainctl, "init"); code != 0 {
		t.Fatalf("got %d, %q", code, stderr)
	}
	if _, err := os.Stat(filepath.Join("notes", ".brain", "config.yaml")); err != nil {
		t.Error(err)
	}
	if _, err := os.Stat(".brain"); err == nil {
		t.Error("wrote .brain in the current directory")
	}
}

// Without BRAIN_HOME, a brain elsewhere is not found: workctl looks in the
// current directory and fails.
func TestBrainHomeUnsetUsesCurrentDirectory(t *testing.T) {
	project(t)
	brain := t.TempDir()
	t.Chdir(brain)
	if code, stderr := runProgram(brainctl, "init"); code != 0 {
		t.Fatalf("brainctl init: got %d, %q", code, stderr)
	}
	t.Chdir(t.TempDir())
	if code, stderr := runProgram(workctl, "init"); code != 1 || !strings.Contains(stderr, "run brainctl init first") {
		t.Errorf("workctl init elsewhere: got %d, %q", code, stderr)
	}
	t.Setenv(brainconfig.EnvVar, brain)
	if code, stderr := runProgram(workctl, "init"); code != 0 {
		t.Errorf("workctl init with %s: got %d, %q", brainconfig.EnvVar, code, stderr)
	}
}

func TestBadBrainHome(t *testing.T) {
	for _, tt := range []struct {
		name    string
		dir     func(t *testing.T) string
		wantErr string
	}{
		{"missing", func(t *testing.T) string { return filepath.Join(t.TempDir(), "typo") }, "BRAIN_HOME="},
		{"a file", func(t *testing.T) string {
			f := filepath.Join(t.TempDir(), "file")
			if err := os.WriteFile(f, nil, 0o644); err != nil {
				t.Fatal(err)
			}
			return f
		}, "not a directory"},
	} {
		for _, prog := range []Program{brainctl, workctl} {
			t.Run(tt.name+"/"+prog.Name, func(t *testing.T) {
				installs := project(t)
				dir := tt.dir(t)
				t.Setenv(brainconfig.EnvVar, dir)
				code, stderr := runProgram(prog, "init")
				if code != 1 || !strings.HasPrefix(stderr, "error: ") || !strings.Contains(stderr, tt.wantErr) {
					t.Errorf("got %d, %q; want an error containing %q", code, stderr, tt.wantErr)
				}
				if *installs != 0 {
					t.Error("install ran after a config error")
				}
				if info, err := os.Stat(dir); tt.name == "missing" && err == nil {
					t.Errorf("made %s: %v", dir, info.Mode())
				}
				cwdIsEmpty(t)
			})
		}
	}
}

func TestUsageMentionsBrainDir(t *testing.T) {
	for _, prog := range []Program{brainctl, workctl} {
		for _, args := range [][]string{{"help"}, {"init", "-h"}} {
			if _, stderr := runProgram(prog, args...); !strings.Contains(stderr, "$BRAIN_HOME") {
				t.Errorf("%s %q does not mention $BRAIN_HOME:\n%s", prog.Name, args, stderr)
			}
		}
	}
}

func TestWorkctlInitSetsItsBin(t *testing.T) {
	project(t)
	writeBrainConfig(t, brainOnly)
	code, stderr := runProgram(workctl, "init")
	if code != 0 || !strings.Contains(stderr, "Set bin: workctl for extension app.cerebrai.workctl in ") {
		t.Fatalf("got %d, %q", code, stderr)
	}
	if got := brainConfig(t); got != withWorkctl {
		t.Errorf("config = %q, want %q", got, withWorkctl)
	}
	if code, stderr := runProgram(workctl, "init"); code != 0 || strings.Contains(stderr, "Set bin") {
		t.Errorf("second run: got %d, %q", code, stderr)
	}
}

// Settings without a bin, as a user may write them: init adds the bin
// without touching the rest.
func TestWorkctlInitSetsBinInExistingSettings(t *testing.T) {
	project(t)
	const settingsOnly = "schema_version: 1\nextensions:\n  app.cerebrai.workctl:\n    schema_version: 1 # mine\n"
	writeBrainConfig(t, settingsOnly)
	code, stderr := runProgram(workctl, "init")
	if code != 0 || !strings.Contains(stderr, "Using existing settings") || !strings.Contains(stderr, "Set bin: workctl") {
		t.Fatalf("got %d, %q", code, stderr)
	}
	if got, want := brainConfig(t), settingsOnly+"    bin: workctl\n"; got != want {
		t.Errorf("config = %q, want %q", got, want)
	}
}

// A bin the user set is kept, whatever it is: an absolute path for a program
// not on PATH, or another name it is installed under.
func TestWorkctlInitKeepsUsersBin(t *testing.T) {
	for _, bin := range []string{"workctl", "/opt/tools/bin/workctl", "workctl-dev"} {
		t.Run(bin, func(t *testing.T) {
			project(t)
			content := "schema_version: 1\nextensions:\n  app.cerebrai.workctl:\n    schema_version: 1\n    bin: " + bin + " # mine\n"
			writeBrainConfig(t, content)
			code, stderr := runProgram(workctl, "init")
			if code != 0 || strings.Contains(stderr, "Set bin") {
				t.Fatalf("got %d, %q", code, stderr)
			}
			if got := brainConfig(t); got != content {
				t.Errorf("config changed to %q", got)
			}
		})
	}
}

// brainctl has no extension settings, so no bin, of its own.
func TestBrainctlInitAddsNoExtension(t *testing.T) {
	project(t)
	if code, stderr := runProgram(brainctl, "init"); code != 0 || strings.Contains(stderr, "Set bin") {
		t.Fatalf("got %d, %q", code, stderr)
	}
	if got := brainConfig(t); got != brainOnly {
		t.Errorf("config = %q", got)
	}
}
