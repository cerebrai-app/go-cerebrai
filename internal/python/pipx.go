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
// Python distributions, and the commands and manual pages it installs in
// <data dir>/pipx/bin (PIPX_BIN_DIR) and <data dir>/pipx/man (PIPX_MAN_DIR),
// unless those variables are set. It returns the command's path.
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

// pipxEnv is the variables the pipx command sets when they are unset.
type pipxEnv struct{ home, binDir, manDir string }

func newPipxEnv(pipxHome string) pipxEnv {
	return pipxEnv{
		home:   pipxHome,
		binDir: filepath.Join(pipxHome, "bin"),
		manDir: filepath.Join(pipxHome, "man"),
	}
}

// vars lists the variables in the order the command sets them.
func (e pipxEnv) vars() [][2]string {
	return [][2]string{{"PIPX_HOME", e.home}, {"PIPX_BIN_DIR", e.binDir}, {"PIPX_MAN_DIR", e.manDir}}
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

	env := newPipxEnv(pipxHome)
	cmd, err := writePipxCommand(bin, python, env, windows)
	if err != nil {
		return "", err
	}
	say("pipx: %s\n  environments in %s unless $PIPX_HOME is set\n"+
		"  commands it installs in %s unless $PIPX_BIN_DIR is set; add that to your PATH\n"+
		"  manual pages in %s unless $PIPX_MAN_DIR is set",
		cmd, env.home, env.binDir, env.manDir)
	return cmd, nil
}

// writePipxCommand writes the pipx command into bin, replacing the one pip
// wrote, and returns its path.
func writePipxCommand(bin, python string, env pipxEnv, windows bool) (string, error) {
	name, script := "pipx", unixPipxScript(python, env)
	if windows {
		name, script = "pipx.cmd", windowsPipxScript(python, env)
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

func unixPipxScript(python string, env pipxEnv) string {
	script := "#!/bin/sh\n" +
		"# Written by brainctl init or workctl init. Runs pipx with its environments\n" +
		"# commands and manual pages next to the Python distributions, unless\n" +
		"# PIPX_HOME, PIPX_BIN_DIR and PIPX_MAN_DIR are set.\n"
	for _, v := range env.vars() {
		script += `if [ -z "${` + v[0] + `:-}" ]; then ` + v[0] + `=` + shQuote(v[1]) + "; fi\n" +
			"export " + v[0] + "\n"
	}
	return script + "exec " + shQuote(python) + ` -I -m pipx "$@"` + "\n"
}

func windowsPipxScript(python string, env pipxEnv) string {
	// In a batch file % starts a variable; %% is a literal %. Windows paths
	// cannot contain ".
	esc := func(s string) string { return strings.ReplaceAll(s, "%", "%%") }
	script := "@echo off\r\n" +
		"rem Written by brainctl init or workctl init. Runs pipx with its environments\r\n" +
		"rem commands and manual pages next to the Python distributions, unless\r\n" +
		"rem PIPX_HOME, PIPX_BIN_DIR and PIPX_MAN_DIR are set.\r\n" +
		"setlocal\r\n"
	for _, v := range env.vars() {
		script += `if not defined ` + v[0] + ` set "` + v[0] + `=` + esc(v[1]) + "\"\r\n"
	}
	return script + `"` + esc(python) + `" -I -m pipx %*` + "\r\n" +
		"exit /b %ERRORLEVEL%\r\n"
}
