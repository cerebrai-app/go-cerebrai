package python

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// fakeDist makes a distribution directory holding only an interpreter file
// in the platform's place and returns the directory, the interpreter and the
// scripts directory.
func fakeDist(t *testing.T, windows bool) (dist, python, bin string) {
	t.Helper()
	dist = filepath.Join(t.TempDir(), "3.14.1+freethreaded+pgo+lto")
	install := filepath.Join(dist, "python", "install")
	python, bin = filepath.Join(install, "bin", "python3"), filepath.Join(install, "bin")
	if windows {
		python, bin = filepath.Join(install, "python.exe"), filepath.Join(install, "Scripts")
	}
	for _, d := range []string{filepath.Dir(python), bin} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(python, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	return dist, python, bin
}

// stubRunPython replaces runPython: "import pipx" succeeds if installed is
// true, and pip install fails with pipErr. It returns the calls made.
func stubRunPython(t *testing.T, installed bool, pipErr error) *[][]string {
	t.Helper()
	var calls [][]string
	old := runPython
	t.Cleanup(func() { runPython = old })
	runPython = func(_ io.Writer, python string, args ...string) error {
		calls = append(calls, append([]string{python}, args...))
		if slices.Contains(args, "import pipx") {
			if installed {
				return nil
			}
			return errors.New("exit status 1")
		}
		return pipErr
	}
	return &calls
}

func TestInstallPipx(t *testing.T) {
	for _, windows := range []bool{false, true} {
		dist, python, bin := fakeDist(t, windows)
		calls := stubRunPython(t, false, nil)
		if windows {
			// pip's own launcher, which would shadow pipx.cmd.
			if err := os.WriteFile(filepath.Join(bin, "pipx.exe"), nil, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		home := filepath.Join(t.TempDir(), "it's 100% pipx")
		cmd, err := installPipx(dist, home, windows, Options{Quiet: true})
		if err != nil {
			t.Fatalf("windows=%v: %v", windows, err)
		}
		want := [][]string{
			{python, "-I", "-c", "import pipx"},
			{python, "-I", "-m", "pip", "install", "--disable-pip-version-check", "--no-warn-script-location", "--root-user-action=ignore", "pipx"},
		}
		if !slices.EqualFunc(*calls, want, slices.Equal) {
			t.Errorf("windows=%v: calls %q", windows, *calls)
		}
		script, err := os.ReadFile(cmd)
		if err != nil {
			t.Fatal(err)
		}
		if windows {
			if cmd != filepath.Join(bin, "pipx.cmd") {
				t.Errorf("command %s", cmd)
			}
			if exists(filepath.Join(bin, "pipx.exe")) {
				t.Error("pipx.exe left in place")
			}
			for _, line := range []string{
				`if not defined PIPX_HOME set "PIPX_HOME=` + strings.ReplaceAll(home, "%", "%%") + "\"\r\n",
				`"` + python + `" -I -m pipx %*` + "\r\n",
			} {
				if !strings.Contains(string(script), line) {
					t.Errorf("pipx.cmd lacks %q:\n%s", line, script)
				}
			}
		} else if cmd != filepath.Join(bin, "pipx") {
			t.Errorf("command %s", cmd)
		}
		entries, _ := os.ReadDir(bin)
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".tmp") {
				t.Errorf("temporary file left: %s", e.Name())
			}
		}
	}
}

func TestInstallPipxSkipsPipWhenInstalled(t *testing.T) {
	dist, _, bin := fakeDist(t, false)
	calls := stubRunPython(t, true, errors.New("pip must not run"))
	if _, err := installPipx(dist, "/pipx", false, Options{Quiet: true}); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 {
		t.Errorf("calls %q", *calls)
	}
	// The command is still written.
	if !exists(filepath.Join(bin, "pipx")) {
		t.Error("no pipx command")
	}
}

func TestInstallPipxErrors(t *testing.T) {
	dist, _, bin := fakeDist(t, false)
	stubRunPython(t, false, errors.New("exit status 1"))
	if _, err := installPipx(dist, "/pipx", false, Options{Quiet: true}); err == nil ||
		!strings.Contains(err.Error(), "installing pipx") {
		t.Errorf("pip failure: %v", err)
	}
	if exists(filepath.Join(bin, "pipx")) {
		t.Error("pipx command written although pip failed")
	}

	stubRunPython(t, false, nil)
	if _, err := installPipx(t.TempDir(), "/pipx", false, Options{Quiet: true}); err == nil ||
		!strings.Contains(err.Error(), "run init again") {
		t.Errorf("no interpreter: %v", err)
	}
}

// TestPipxScriptRuns runs the POSIX command with a stand-in interpreter that
// prints PIPX_HOME and its arguments.
func TestPipxScriptRuns(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell script")
	}
	dir := t.TempDir()
	python := filepath.Join(dir, "it's python")
	if err := os.WriteFile(python, []byte("#!/bin/sh\necho \"$PIPX_HOME|$*\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(dir, "it's $HOME")
	cmd, err := writePipxCommand(dir, python, home, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ env, want string }{
		{"", home + "|-I -m pipx install some tool"},
		{"/elsewhere", "/elsewhere|-I -m pipx install some tool"},
	} {
		c := exec.Command(cmd, "install", "some tool")
		c.Env = append(os.Environ(), "PIPX_HOME="+tc.env)
		out, err := c.Output()
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(string(out)); got != tc.want {
			t.Errorf("PIPX_HOME=%q: got %q, want %q", tc.env, got, tc.want)
		}
	}
}
