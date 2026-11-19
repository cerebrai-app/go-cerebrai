//go:build integration

package python

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestIntegrationInstallAndPipx downloads a real distribution with Install,
// installs pipx into it with InstallPipx, then installs a tool with that pipx,
// checking that each step's output lands where it should. It needs network
// access and downloads about 100 MB, so it runs only with -tags integration.
func TestIntegrationInstallAndPipx(t *testing.T) {
	tmp := t.TempDir()
	data := filepath.Join(tmp, "data")
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	// XDG_DATA_HOME is honoured on every platform. HOME and USERPROFILE point
	// at an empty directory, so pipx's own defaults, were they used, would show
	// up there.
	t.Setenv("XDG_DATA_HOME", data)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("PIPX_HOME", "")
	t.Setenv("PIPX_BIN_DIR", "")
	t.Setenv("PIPX_MAN_DIR", "")
	var log bytes.Buffer
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("log:\n%s", log.String())
		}
	})
	opts := Options{Log: &log}

	dist, err := Install(opts)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(dist) != filepath.Join(data, "python-build-standalone") {
		t.Errorf("distribution in %s", dist)
	}
	python := filepath.Join(dist, "python", "install", "bin", "python3")
	if runtime.GOOS == "windows" {
		python = filepath.Join(dist, "python", "install", "python.exe")
	}
	version := output(t, python, "-I", "-c", "import sys; print('%d.%d' % sys.version_info[:2])")
	if version != DefaultPythonVersion {
		t.Errorf("Python %s, want %s", version, DefaultPythonVersion)
	}

	pipx, err := InstallPipx(dist, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log.String(), "installing pipx into "+dist) {
		t.Error("pip did not run")
	}
	pipxHome := filepath.Join(data, "pipx")
	binDir := filepath.Join(pipxHome, "bin")
	manDir := filepath.Join(pipxHome, "man")
	if got := output(t, pipx, "environment", "--value", "PIPX_HOME"); got != pipxHome {
		t.Errorf("PIPX_HOME %s, want %s", got, pipxHome)
	}
	if got := output(t, pipx, "environment", "--value", "PIPX_BIN_DIR"); got != binDir {
		t.Errorf("PIPX_BIN_DIR %s, want %s", got, binDir)
	}
	if got := output(t, pipx, "environment", "--value", "PIPX_MAN_DIR"); got != manDir {
		t.Errorf("PIPX_MAN_DIR %s, want %s", got, manDir)
	}

	// A second run finds pipx and leaves pip alone.
	log.Reset()
	if _, err := InstallPipx(dist, opts); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log.String(), "pipx already installed in "+dist) {
		t.Errorf("second run: %s", log.String())
	}

	output(t, pipx, "install", "pycowsay")
	if !exists(filepath.Join(pipxHome, "venvs", "pycowsay")) {
		t.Errorf("no pycowsay environment in %s", pipxHome)
	}
	cowsay := filepath.Join(binDir, "pycowsay")
	if runtime.GOOS == "windows" {
		cowsay += ".exe"
	}
	if got := output(t, cowsay, "moo from pipx"); !strings.Contains(got, "moo from pipx") {
		t.Errorf("pycowsay printed %q", got)
	}
	if !exists(filepath.Join(manDir, "man6", "pycowsay.6")) {
		t.Errorf("no pycowsay manual page in %s", manDir)
	}
	// pip's download cache may go to ~/.cache; nothing of pipx's may.
	for _, d := range []string{".local", "pipx", filepath.Join("AppData", "Local", "pipx")} {
		if exists(filepath.Join(home, d)) {
			t.Errorf("pipx wrote to its default directory %s", filepath.Join(home, d))
		}
	}
}

// output runs name with args and returns its trimmed standard output, failing
// the test, with the standard error, if it fails.
func output(t *testing.T, name string, args ...string) string {
	t.Helper()
	var stderr bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s %q: %v\n%s", name, args, err, stderr.String())
	}
	return strings.TrimSpace(string(out))
}
