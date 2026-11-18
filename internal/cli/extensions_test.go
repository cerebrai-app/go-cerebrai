package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"unicode"

	"github.com/cerebrai-app/go-cerebrai/brainconfig"
)

// fakeBin writes an executable called name in dir and returns its path.
func fakeBin(t *testing.T, dir, name string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// binDir makes a directory of executables, sets PATH to just it, and returns
// it.
func binDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	return dir
}

// runBrainctl runs brainctl with args and returns its exit code, stdout and
// stderr.
func runBrainctl(args ...string) (int, string, string) {
	var out, errOut strings.Builder
	code := Main(brainctl, args, &out, &errOut)
	return code, out.String(), errOut.String()
}

// rows splits the table brainctl extensions ls printed into its rows'
// columns, which are separated by two or more spaces.
func rows(t *testing.T, table string) [][]string {
	t.Helper()
	var rows [][]string
	for _, line := range strings.Split(strings.TrimSuffix(table, "\n"), "\n") {
		rows = append(rows, columnGap.Split(strings.TrimRight(line, " "), -1))
	}
	return rows
}

var columnGap = regexp.MustCompile(` {2,}`)

func TestExtensionsList(t *testing.T) {
	project(t)
	bin := binDir(t)
	workctlPath := fakeBin(t, bin, "workctl")
	toolPath := fakeBin(t, t.TempDir(), "tool")
	missingAbs := filepath.Join(t.TempDir(), "gone")
	writeBrainConfig(t, fmt.Sprintf("schema_version: 1\nextensions:\n"+
		"  app.cerebrai.workctl: {schema_version: 1, name: workctl, bin: workctl}\n"+
		"  com.example.tool: {schema_version: 3, name: Example Tool, bin: %q, color: blue}\n"+
		"  com.example.absent: {schema_version: 1, bin: not-installed}\n"+
		"  com.example.gone: {schema_version: 1, bin: %q}\n"+
		"  com.example.nobin: {schema_version: 1, name: No Bin}\n"+
		"  com.example.badname: {schema_version: 1, name: '', bin: workctl}\n"+
		"  com.example.badbin: {schema_version: 1, bin: a/b}\n"+
		"  com.example.broken: not a mapping\n"+
		"  com.example.multiline: {schema_version: 1, name: [a, b]}\n"+
		"  com.example.newer: {schema_version: 99, bin: workctl}\n",
		toolPath, missingAbs))
	want := [][]string{
		{"EXTENSION", "NAME", "BIN", "PATH"},
		{"app.cerebrai.workctl", "workctl", "workctl", workctlPath},
		{"com.example.absent", "-", "not-installed", "not found"},
		{"com.example.badbin", "-", "-", `invalid settings for extension com.example.badbin: invalid bin "a/b": use a bare executable name or an absolute path`},
		{"com.example.badname", "-", "-", "invalid settings for extension com.example.badname: invalid name: empty"},
		{"com.example.broken", "-", "-", "invalid config file: the settings for extension com.example.broken must be a mapping"},
		{"com.example.gone", "-", missingAbs, "not found"},
		{"com.example.multiline", "-", "-", "invalid settings for extension com.example.multiline: yaml: unmarshal errors: line 11: cannot unmarshal !!seq into string"},
		{"com.example.newer", "-", "workctl", workctlPath},
		{"com.example.nobin", "No Bin", "-", "no bin set"},
		{"com.example.tool", "Example Tool", toolPath, toolPath},
	}
	for _, alias := range []string{"l", "ls", "list"} {
		code, stdout, stderr := runBrainctl("extensions", alias)
		if code != 0 || stderr != "" {
			t.Errorf("%s: got %d, stderr %q", alias, code, stderr)
		}
		if got := rows(t, stdout); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: rows =\n%q\nwant\n%q", alias, got, want)
		}
	}
}

func TestExtensionsListAfterInit(t *testing.T) {
	project(t)
	workctlPath := fakeBin(t, binDir(t), "workctl")
	for _, prog := range []Program{brainctl, workctl} {
		if code, stderr := runProgram(prog, "init"); code != 0 {
			t.Fatalf("%s init: got %d, %q", prog.Name, code, stderr)
		}
	}
	code, stdout, stderr := runBrainctl("extensions", "ls")
	if want := "EXTENSION             NAME     BIN      PATH\napp.cerebrai.workctl  workctl  workctl  " + workctlPath + "\n"; code != 0 || stdout != want || stderr != "" {
		t.Errorf("got %d, %q, %q; want stdout %q", code, stdout, stderr, want)
	}
}

func TestExtensionsListUsesBrainHome(t *testing.T) {
	project(t)
	brain := brainAt(t)
	workctlPath := fakeBin(t, binDir(t), "workctl")
	if err := os.MkdirAll(filepath.Join(brain, ".brain"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(brainconfig.Path(brain), []byte(withWorkctl), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, stdout, stderr := runBrainctl("extensions", "ls"); code != 0 || !strings.Contains(stdout, workctlPath) {
		t.Errorf("got %d, %q, %q", code, stdout, stderr)
	}
}

func TestExtensionsListEmpty(t *testing.T) {
	for _, content := range []string{brainOnly, "schema_version: 1\nextensions:\n", "schema_version: 1\nextensions: {}\n"} {
		project(t)
		writeBrainConfig(t, content)
		code, stdout, stderr := runBrainctl("extensions", "ls")
		if code != 0 || stdout != "" || !strings.Contains(stderr, "No extensions have settings in ") {
			t.Errorf("%q: got %d, %q, %q", content, code, stdout, stderr)
		}
	}
}

func TestExtensionsListConfigErrors(t *testing.T) {
	for _, tt := range []struct {
		name, content, wantErr string // content "" means no config file
	}{
		{"no config", "", "run brainctl init first"},
		{"newer config", "schema_version: 2\n", "upgrade to a newer release"},
		{"extensions not a mapping", "schema_version: 1\nextensions: [a]\n", "invalid config file"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			project(t)
			if tt.content != "" {
				writeBrainConfig(t, tt.content)
			}
			code, stdout, stderr := runBrainctl("extensions", "ls")
			if code != 1 || stdout != "" || !strings.HasPrefix(stderr, "error: ") || !strings.Contains(stderr, tt.wantErr) {
				t.Errorf("got %d, %q, %q; want an error containing %q", code, stdout, stderr, tt.wantErr)
			}
		})
	}
	project(t)
	t.Setenv(brainconfig.EnvVar, filepath.Join(t.TempDir(), "typo"))
	if code, _, stderr := runBrainctl("extensions", "ls"); code != 1 || !strings.Contains(stderr, "BRAIN_HOME=") {
		t.Errorf("bad BRAIN_HOME: got %d, %q", code, stderr)
	}
}

// An extension that is not installed is listed, not an error: the same file
// is on every machine, and not every machine has every extension.
func TestExtensionsListNothingInstalled(t *testing.T) {
	project(t)
	binDir(t)
	writeBrainConfig(t, withWorkctl)
	code, stdout, _ := runBrainctl("extensions", "ls")
	if want := [][]string{{"EXTENSION", "NAME", "BIN", "PATH"}, {"app.cerebrai.workctl", "workctl", "workctl", "not found"}}; code != 0 || !reflect.DeepEqual(rows(t, stdout), want) {
		t.Errorf("got %d, %q", code, stdout)
	}
}

func TestExtensionsListNotExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no executable bit")
	}
	project(t)
	binDir(t)
	file := filepath.Join(t.TempDir(), "tool")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	writeBrainConfig(t, fmt.Sprintf("schema_version: 1\nextensions:\n  com.example.tool: {schema_version: 1, bin: %q}\n", file))
	if code, stdout, _ := runBrainctl("extensions", "ls"); code != 0 || !strings.HasSuffix(stdout, "  not executable\n") {
		t.Errorf("got %d, %q", code, stdout)
	}
}

func TestWhere(t *testing.T) {
	old := lookPath
	t.Cleanup(func() { lookPath = old })
	for _, tt := range []struct {
		err  error
		want string
	}{
		{nil, "/found/x"},
		{&exec.Error{Name: "x", Err: exec.ErrNotFound}, "not found"},
		{&exec.Error{Name: "x", Err: os.ErrNotExist}, "not found"},
		{&exec.Error{Name: "x", Err: exec.ErrDot}, "not found (only in the current directory, through a relative PATH entry)"},
		{&exec.Error{Name: "x", Err: os.ErrPermission}, "not executable"},
		{errors.New("disk on fire"), "cannot look up: disk on fire"},
	} {
		lookPath = func(string) (string, error) {
			if tt.err != nil {
				return "", tt.err
			}
			return "/found/x", nil
		}
		if got := where("x"); got != tt.want {
			t.Errorf("%v: where = %q, want %q", tt.err, got, tt.want)
		}
	}
}

func TestExtensionsUsage(t *testing.T) {
	project(t)
	writeBrainConfig(t, brainOnly)
	for _, args := range [][]string{
		{"extensions"}, {"extensions", "bogus"}, {"extensions", "ls", "extra"}, {"extensions", "list", "-a"},
	} {
		code, stdout, stderr := runBrainctl(args...)
		if code != 2 || stdout != "" || !strings.Contains(stderr, "usage: brainctl extensions <command>") {
			t.Errorf("%q: got %d, %q, %q", args, code, stdout, stderr)
		}
	}
	for _, args := range [][]string{{"extensions", "-h"}, {"extensions", "help"}, {"extensions", "--help"}} {
		code, stdout, stderr := runBrainctl(args...)
		if code != 0 || stdout != "" || !strings.Contains(stderr, "l, ls, list") || !strings.Contains(stderr, "$BRAIN_HOME") {
			t.Errorf("%q: got %d, %q, %q", args, code, stdout, stderr)
		}
	}
	if _, _, stderr := runBrainctl("help"); !strings.Contains(stderr, "extensions  list the extensions in the brain config") {
		t.Errorf("brainctl help does not mention extensions:\n%s", stderr)
	}
}

// Only brainctl has the command: its extensions have no extensions.
func TestWorkctlHasNoExtensionsCommand(t *testing.T) {
	project(t)
	writeBrainConfig(t, withWorkctl)
	code, stderr := runProgram(workctl, "extensions", "ls")
	if code != 2 || !strings.Contains(stderr, `unknown command "extensions"`) {
		t.Errorf("got %d, %q", code, stderr)
	}
	if _, stderr := runProgram(workctl, "help"); strings.Contains(stderr, "extensions") {
		t.Errorf("workctl help mentions extensions:\n%s", stderr)
	}
}

func TestExtensionsListWriteError(t *testing.T) {
	project(t)
	writeBrainConfig(t, withWorkctl)
	var errOut strings.Builder
	if code := Main(brainctl, []string{"extensions", "ls"}, failingWriter{}, &errOut); code != 1 || !strings.HasPrefix(errOut.String(), "error: ") {
		t.Errorf("got %d, %q", code, errOut.String())
	}
}

// Text from the config never reaches the terminal as control characters:
// they are shown as escapes, so a hostile config cannot clear the screen or
// break the table.
func TestExtensionsListEscapesControlCharacters(t *testing.T) {
	project(t)
	binDir(t)
	writeBrainConfig(t, "schema_version: 1\nextensions:\n"+
		"  \"com.example.\\e[2Jtab\\tid\": {schema_version: 1, bin: tool}\n"+
		"  com.example.badname: {schema_version: 1, name: \"x\\e[31my\"}\n")
	code, stdout, stderr := runBrainctl("extensions", "ls")
	if code != 0 {
		t.Fatalf("got %d, %q", code, stderr)
	}
	if strings.ContainsFunc(stdout, func(r rune) bool { return r != '\n' && unicode.IsControl(r) }) {
		t.Errorf("control characters reached the output: %q", stdout)
	}
	for _, want := range []string{`com.example.\x1b[2Jtab\tid`, `control characters are not allowed`} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output lacks %q:\n%s", want, stdout)
		}
	}
}

func TestCell(t *testing.T) {
	for in, want := range map[string]string{
		"plain":      "plain",
		"Ünïcode ✓":  "Ünïcode ✓",
		"a\x1b[2Jb":  `a\x1b[2Jb`,
		"a\tb\nc":    `a\tb\nc`,
		"\x00\u0085": `\x00\u0085`,
	} {
		if got := cell(in); got != want {
			t.Errorf("cell(%q) = %q, want %q", in, got, want)
		}
	}
}
