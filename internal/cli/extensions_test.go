package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

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

func TestExtensionsList(t *testing.T) {
	project(t)
	bin := binDir(t)
	workctlPath := fakeBin(t, bin, "workctl")
	toolPath := fakeBin(t, t.TempDir(), "tool")
	missingAbs := filepath.Join(t.TempDir(), "gone")
	writeBrainConfig(t, fmt.Sprintf("schema_version: 1\nextension_bins:\n  - workctl\n  - %q\n  - not-installed\n  - %q\n",
		toolPath, missingAbs))
	want := "NAME" + strings.Repeat(" ", len(missingAbs)-4+2) + "PATH\n" +
		"workctl" + strings.Repeat(" ", len(missingAbs)-7+2) + workctlPath + "\n" +
		toolPath + strings.Repeat(" ", len(missingAbs)-len(toolPath)+2) + toolPath + "\n" +
		"not-installed" + strings.Repeat(" ", len(missingAbs)-13+2) + "not found\n" +
		missingAbs + "  not found\n"
	for _, alias := range []string{"l", "ls", "list"} {
		code, stdout, stderr := runBrainctl("extensions", alias)
		if code != 0 || stderr != "" {
			t.Errorf("%s: got %d, stderr %q", alias, code, stderr)
		}
		if stdout != want {
			t.Errorf("%s: stdout =\n%s\nwant\n%s", alias, stdout, want)
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
	if want := "NAME     PATH\nworkctl  " + workctlPath + "\n"; code != 0 || stdout != want || stderr != "" {
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
	if err := os.WriteFile(brainconfig.Path(brain), []byte("schema_version: 1\nextension_bins: [workctl]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, stdout, stderr := runBrainctl("extensions", "ls"); code != 0 || !strings.Contains(stdout, workctlPath) {
		t.Errorf("got %d, %q, %q", code, stdout, stderr)
	}
}

func TestExtensionsListEmpty(t *testing.T) {
	for _, content := range []string{brainOnly, "schema_version: 1\nextension_bins: []\n", withWorkctl[:strings.Index(withWorkctl, "extension_bins")]} {
		project(t)
		writeBrainConfig(t, content)
		code, stdout, stderr := runBrainctl("extensions", "ls")
		if code != 0 || stdout != "" || !strings.Contains(stderr, "No extensions are listed in extension_bins in ") {
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
		{"bad extension_bins", "schema_version: 1\nextension_bins: [a, a]\n", "listed twice"},
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

// An entry that is not installed is listed, not an error: the same file is
// on every machine, and not every machine has every extension.
func TestExtensionsListNothingInstalled(t *testing.T) {
	project(t)
	binDir(t)
	writeBrainConfig(t, withWorkctl)
	code, stdout, _ := runBrainctl("extensions", "ls")
	if code != 0 || stdout != "NAME     PATH\nworkctl  not found\n" {
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
	writeBrainConfig(t, fmt.Sprintf("schema_version: 1\nextension_bins: [%q]\n", file))
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
	if _, _, stderr := runBrainctl("help"); !strings.Contains(stderr, "extensions  list the extension executables") {
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
