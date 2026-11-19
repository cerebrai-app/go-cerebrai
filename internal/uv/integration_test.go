//go:build integration

package uv

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestIntegrationInstallAndPython writes the environment file with
// WriteEnvFile, downloads the real uv with Install and a real Python with
// InstallPython into a temporary brainctl directory, then
// installs a tool with that uv, checking that everything runs and lands in the
// brainctl directory, whatever the user's own uv settings say. It needs network
// access, so it runs only with -tags integration.
func TestIntegrationInstallAndPython(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	// The user's own uv setup, which must be left alone: settings files and
	// variables pointing at their directories.
	decoy := filepath.Join(tmp, "decoy")
	config := "python-install-dir = '" + filepath.ToSlash(filepath.Join(decoy, "config-python")) + "'\n" +
		"cache-dir = '" + filepath.ToSlash(filepath.Join(decoy, "config-cache")) + "'\n"
	for _, dir := range []string{filepath.Join(home, "config", "uv"), filepath.Join(home, "appdata", "uv")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "uv.toml"), []byte(config), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("APPDATA", filepath.Join(home, "appdata"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "localappdata"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(decoy, "xdg-data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(decoy, "xdg-cache"))
	t.Setenv("XDG_BIN_HOME", filepath.Join(decoy, "xdg-bin"))
	t.Setenv("UV_PYTHON_INSTALL_DIR", filepath.Join(decoy, "env-python"))
	t.Setenv("UV_TOOL_BIN_DIR", filepath.Join(decoy, "env-bin"))
	t.Setenv("UV_CACHE_DIR", filepath.Join(decoy, "env-cache"))
	t.Setenv("UV_PYTHON", "3.12")

	d, err := DefaultDirs()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".local", "opt", "brainctl")
	if runtime.GOOS == "windows" {
		want = filepath.Join(home, "localappdata", "brainctl")
	}
	if d.Root != want {
		t.Errorf("brainctl directory %s, want %s", d.Root, want)
	}

	// An environment file edited by hand: a variable of the user's that must
	// be kept, and a required one pointing elsewhere that must be put back.
	if err := os.MkdirAll(d.Root, 0o755); err != nil {
		t.Fatal(err)
	}
	edited := "UV_CACHE_DIR=" + filepath.Join(decoy, "file-cache") + "\nBRAINCTL_TEST=from the env file\n"
	if err := os.WriteFile(d.EnvFile(), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteEnvFile(d); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, d.EnvFile()); !strings.Contains(got, "UV_CACHE_DIR="+d.Cache+"\n") ||
		!strings.Contains(got, "BRAINCTL_TEST=from the env file\n") {
		t.Errorf("env file:\n%s", got)
	}

	var log bytes.Buffer
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("log:\n%s", log.String())
		}
	})
	opts := Options{Log: &log}

	// uv itself.
	if err := Install(d, opts); err != nil {
		t.Fatal(err)
	}
	if got := run(t, d, d.UV(), "--version"); !strings.HasPrefix(got, "uv "+Version+" ") {
		t.Errorf("uv --version printed %q", got)
	}
	if got := run(t, d, filepath.Join(d.Bin, exe("uvx")), "--version"); !strings.HasPrefix(got, "uvx "+Version+" ") {
		t.Errorf("uvx --version printed %q", got)
	}
	// A second run keeps it.
	log.Reset()
	if err := Install(d, opts); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log.String(), "uv "+Version+" already installed") {
		t.Errorf("second Install: %s", log.String())
	}

	// The default Python: 3.14, free-threaded.
	python, err := InstallPython(d, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !within(d.Python, python) {
		t.Errorf("Python at %s, not in %s", python, d.Python)
	}
	if got := run(t, d, python, "-I", "-c",
		"import sys; print('%d.%d' % sys.version_info[:2], sys._is_gil_enabled())"); got != "3.14 False" {
		t.Errorf("Python reports %q, want 3.14 without the GIL", got)
	}
	// What the processes brainctl runs see: the env file's variables.
	if got := run(t, d, python, "-I", "-c",
		"import os; print(os.environ['BRAINCTL_TEST'], os.environ['UV_CACHE_DIR'], 'UV_PYTHON' in os.environ)"); got != "from the env file "+d.Cache+" False" {
		t.Errorf("Python sees %q", got)
	}
	if _, err := os.Stat(filepath.Join(d.Bin, exe("python3.14t"))); err != nil {
		t.Errorf("no python3.14t command: %v", err)
	}
	// Installed already: uv says so and downloads nothing.
	if _, err := InstallPython(d, opts); err != nil {
		t.Fatal(err)
	}

	// A tool, with that Python.
	run(t, d, d.UV(), "tool", "install", "pycowsay")
	if _, err := os.Stat(filepath.Join(d.Tools, "pycowsay")); err != nil {
		t.Errorf("no pycowsay environment in %s: %v", d.Tools, err)
	}
	if got := run(t, d, filepath.Join(d.Bin, exe("pycowsay")), "moo from uv"); !strings.Contains(got, "moo from uv") {
		t.Errorf("pycowsay printed %q", got)
	}
	if got := run(t, d, d.UV(), "cache", "dir"); got != d.Cache {
		t.Errorf("uv cache dir %s, want %s", got, d.Cache)
	}

	// Nothing went anywhere else.
	if _, err := os.Stat(decoy); err == nil {
		_ = filepath.WalkDir(decoy, func(p string, _ fs.DirEntry, _ error) error {
			t.Errorf("uv wrote to the user's directories: %s", p)
			return nil
		})
	}
	_ = filepath.WalkDir(home, func(p string, e fs.DirEntry, _ error) error {
		if e != nil && !e.IsDir() && !within(d.Root, p) && filepath.Base(p) != "uv.toml" {
			t.Errorf("uv wrote outside the brainctl directory: %s", p)
		}
		return nil
	})
}

// within reports whether path is in dir.
func within(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// run runs name with args the way brainctl does, through d.Command, and
// returns its trimmed standard output, failing the test with its standard
// error if it fails.
func run(t *testing.T, d Dirs, name string, args ...string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd, err := d.Command(&stderr, name, args...)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %q: %v\n%s", name, args, err, stderr.String())
	}
	return strings.TrimSpace(stdout.String())
}
