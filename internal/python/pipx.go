package python

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// PipxPackage is what InstallPipx installs with pip.
const PipxPackage = "pipx"

// runPython runs the interpreter python with args, sending its output to log.
// Tests replace it.
var runPython = func(log io.Writer, python string, args ...string) error {
	cmd := exec.Command(python, args...)
	cmd.Stdout = log
	cmd.Stderr = log
	// Keep pip installing into the distribution, whatever the user's pip
	// configuration says.
	cmd.Env = append(os.Environ(), "PIP_USER=0", "PIP_REQUIRE_VIRTUALENV=0")
	return cmd.Run()
}

// InstallPipx installs pipx into the distribution Install returned, dist, with
// the distribution's own pip, and replaces the pipx command pip writes with one
// that keeps pipx's environments in <data dir>/pipx (PIPX_HOME), next to the
// Python distributions, unless $PIPX_HOME is set. It returns the command's path.
//
// pip only runs, and so only needs network access, if pipx is not installed in
// dist yet. The command is rewritten every time.
func InstallPipx(dist string, opts Options) (string, error) {
	home, err := dataHome(os.Getenv("XDG_DATA_HOME"), os.Getenv("HOME"),
		os.Getenv("LOCALAPPDATA"), os.Getenv("USERPROFILE"), runtime.GOOS == "windows")
	if err != nil {
		return "", err
	}
	return installPipx(dist, filepath.Join(home, "pipx"), runtime.GOOS == "windows", opts)
}

// installPipx is InstallPipx with PIPX_HOME and the platform given.
func installPipx(dist, pipxHome string, windows bool, opts Options) (string, error) {
	log := opts.log()
	say := func(format string, args ...any) { _, _ = fmt.Fprintf(log, format+"\n", args...) }

	install := filepath.Join(dist, "python", "install")
	python, bin := filepath.Join(install, "bin", "python3"), filepath.Join(install, "bin")
	if windows {
		python, bin = filepath.Join(install, "python.exe"), filepath.Join(install, "Scripts")
	}
	if !exists(python) {
		return "", fmt.Errorf("%s: no Python interpreter there; delete %s and run init again", python, dist)
	}

	// -I: ignore PYTHON* variables, the user's site-packages and the current
	// directory, so nothing outside the distribution changes what runs.
	if runPython(io.Discard, python, "-I", "-c", "import pipx") == nil {
		say("pipx already installed in %s", dist)
	} else {
		say("installing pipx into %s", dist)
		if err := runPython(log, python, "-I", "-m", "pip", "install",
			"--disable-pip-version-check", "--no-warn-script-location", "--root-user-action=ignore", PipxPackage); err != nil {
			return "", fmt.Errorf("installing pipx with %s: %w", python, err)
		}
	}

	cmd, err := writePipxCommand(bin, python, pipxHome, windows)
	if err != nil {
		return "", err
	}
	say("pipx: %s (environments in %s unless $PIPX_HOME is set)", cmd, pipxHome)
	return cmd, nil
}

// writePipxCommand writes the pipx command into bin, replacing the one pip
// wrote, and returns its path.
func writePipxCommand(bin, python, pipxHome string, windows bool) (string, error) {
	name, script := "pipx", unixPipxScript(python, pipxHome)
	if windows {
		name, script = "pipx.cmd", windowsPipxScript(python, pipxHome)
	}
	path := filepath.Join(bin, name)
	tmp := path + "." + uniqueSuffix() + ".tmp"
	if err := os.WriteFile(tmp, []byte(script), 0o755); err != nil {
		return "", fmt.Errorf("%s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("%s: %w", path, err)
	}
	if windows {
		// pip's pipx.exe would run first: .exe comes before .cmd in PATHEXT.
		if err := os.Remove(filepath.Join(bin, "pipx.exe")); err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("%s: %w", filepath.Join(bin, "pipx.exe"), err)
		}
	}
	return path, nil
}

// shQuote quotes s for a POSIX shell.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func unixPipxScript(python, pipxHome string) string {
	return "#!/bin/sh\n" +
		"# Written by brainctl init or workctl init. Runs pipx with its environments next to the\n" +
		"# Python distributions, unless PIPX_HOME is set.\n" +
		`if [ -z "${PIPX_HOME:-}" ]; then PIPX_HOME=` + shQuote(pipxHome) + "; fi\n" +
		"export PIPX_HOME\n" +
		"exec " + shQuote(python) + ` -I -m pipx "$@"` + "\n"
}

func windowsPipxScript(python, pipxHome string) string {
	// In a batch file % starts a variable; %% is a literal %. Windows paths
	// cannot contain ".
	esc := func(s string) string { return strings.ReplaceAll(s, "%", "%%") }
	return "@echo off\r\n" +
		"rem Written by brainctl init or workctl init. Runs pipx with its environments next to the\r\n" +
		"rem Python distributions, unless PIPX_HOME is set.\r\n" +
		"setlocal\r\n" +
		`if not defined PIPX_HOME set "PIPX_HOME=` + esc(pipxHome) + "\"\r\n" +
		`"` + esc(python) + `" -I -m pipx %*` + "\r\n" +
		"exit /b %ERRORLEVEL%\r\n"
}
