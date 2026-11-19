package cli

import (
	"errors"
	"io"
	"runtime"
	"strings"
	"testing"

	"github.com/cerebrai-app/go-cerebrai/brainconfig"
	"github.com/cerebrai-app/go-cerebrai/internal/buildinfo"
	"github.com/cerebrai-app/go-cerebrai/internal/python"
)

var (
	brainctl = Program{Name: "brainctl"}
	workctl  = Program{Name: "workctl", ExtensionID: "app.cerebrai.workctl", ExtensionSchemaVersion: 1}
)

func run(args ...string) (code int, stdout, stderr string) {
	var out, errOut strings.Builder
	code = Main(brainctl, args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestVersion(t *testing.T) {
	code, stdout, stderr := run("version")
	if code != 0 || stdout != "brainctl "+buildinfo.Version()+"\n" || stderr != "" {
		t.Errorf("got %d, %q, %q", code, stdout, stderr)
	}
}

func TestUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		nil, {"nope"}, {"version", "extra"}, {"init", "extra"}, {"init", "--no-such-flag"},
	} {
		code, stdout, stderr := run(args...)
		if code != 2 || stdout != "" || !strings.Contains(stderr, "usage: brainctl") {
			t.Errorf("%q: got %d, %q, %q", args, code, stdout, stderr)
		}
	}
	for _, args := range [][]string{{"help"}, {"--help"}, {"init", "-h"}} {
		if code, _, stderr := run(args...); code != 0 || !strings.Contains(stderr, "usage: brainctl") {
			t.Errorf("%q: got %d, %q", args, code, stderr)
		}
	}
}

// stubInstall replaces install and runs the test in an empty current
// directory, so init's config file goes there.
func stubInstall(t *testing.T, err error) *python.Options {
	t.Helper()
	t.Chdir(t.TempDir())
	// The brain home is the current directory unless a test sets this.
	t.Setenv(brainconfig.EnvVar, "")
	var got python.Options
	old := install
	t.Cleanup(func() { install = old })
	install = func(opts python.Options) (string, error) {
		got = opts
		return "/data/python-build-standalone/x", err
	}
	return &got
}

func TestInitPassesOptions(t *testing.T) {
	got := stubInstall(t, nil)
	if code, stdout, stderr := run("init"); code != 0 || stdout != "" {
		t.Fatalf("got %d, %q, %q", code, stdout, stderr)
	}
	if got.PythonVersion != python.DefaultPythonVersion || got.Variant != "" || got.Log == nil {
		t.Errorf("defaults: %+v", *got)
	}
	for _, args := range [][]string{
		{"init", "--python-version", "3.13.9", "--variant", "pgo+lto"},
		{"init", "--python-version=3.13.9", "-variant=pgo+lto"},
	} {
		if code, _, stderr := run(args...); code != 0 {
			t.Fatalf("%q: got %d, %q", args, code, stderr)
		}
		if got.PythonVersion != "3.13.9" || got.Variant != "pgo+lto" {
			t.Errorf("%q: %+v", args, *got)
		}
	}
}

func TestInitReportsInstallErrors(t *testing.T) {
	stubInstall(t, errors.New("no network"))
	code, stdout, stderr := run("init")
	if code != 1 || stdout != "" || !strings.HasSuffix(stderr, "\nerror: no network\n") {
		t.Errorf("got %d, %q, %q", code, stdout, stderr)
	}
}

func TestVersionWriteError(t *testing.T) {
	var errOut strings.Builder
	if code := Main(brainctl, []string{"version"}, failingWriter{}, &errOut); code != 1 {
		t.Errorf("got %d, stderr %q", code, errOut.String())
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestInitTellsHowToSetBrainHome(t *testing.T) {
	stubInstall(t, nil)
	// An unknown shell, so the result doesn't depend on the machine (but on
	// Windows, setx is suggested whatever the shell).
	t.Setenv("SHELL", "")
	_, _, stderr := run("init")
	want := "Set it to " + mustGetwd(t) + " in your shell's startup file."
	if runtime.GOOS == "windows" {
		want = "Run this once, then open a new terminal:\n\n    setx " + brainconfig.EnvVar
	}
	if !strings.Contains(stderr, "brainctl is ready in "+mustGetwd(t)) || !strings.Contains(stderr, want) {
		t.Errorf("want %q in %q", want, stderr)
	}
	// Already set: nothing to tell.
	t.Setenv(brainconfig.EnvVar, mustGetwd(t))
	if _, _, stderr := run("init"); strings.Contains(stderr, "Next, set") {
		t.Errorf("hint with %s set: %q", brainconfig.EnvVar, stderr)
	}
}

func TestShellHint(t *testing.T) {
	for _, tc := range []struct{ goos, shell, dir, profile, line string }{
		{"darwin", "/bin/zsh", "/Users/me/notes", "~/.zshrc", "export BRAIN_HOME='/Users/me/notes'"},
		{"linux", "/usr/bin/bash", "/home/me/my notes", "~/.bashrc", "export BRAIN_HOME='/home/me/my notes'"},
		{"darwin", "/bin/bash", "/x", "~/.bash_profile", "export BRAIN_HOME='/x'"},
		{"linux", "/usr/bin/fish", `/it's\`, "~/.config/fish/config.fish", `set -gx BRAIN_HOME '/it\'s\\'`},
		{"linux", "/bin/sh", "/it's", "~/.profile", `export BRAIN_HOME='/it'\''s'`},
		{"linux", "/usr/bin/tcsh", "/x", "", ""},
		{"linux", "", "/x", "", ""},
		{"windows", "", `C:\notes`, "", `setx BRAIN_HOME "C:\notes"`},
		{"windows", "", `C:\`, "", `setx BRAIN_HOME "C:\\"`},
	} {
		profile, line := shellHint(tc.goos, tc.shell, tc.dir)
		if profile != tc.profile || line != tc.line {
			t.Errorf("%s %q: got %q, %q", tc.goos, tc.shell, profile, line)
		}
	}
}
