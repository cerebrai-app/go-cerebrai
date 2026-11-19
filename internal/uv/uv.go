// Package uv installs a pinned release of uv (https://docs.astral.sh/uv/)
// into the brainctl directory, ~/.local/opt/brainctl (%LOCALAPPDATA%\brainctl
// on Windows), and installs Python with it. It is shared by brainctl and
// workctl.
//
// Everything uv makes stays in the brainctl directory, so it never touches a
// uv the user installed, or that uv's Pythons, tools, cache or settings:
//
//	bin/     uv and uvx, and the commands of Pythons and tools uv installs
//	python/  Pythons uv installs
//	tools/   tools uv installs
//	cache/   uv's cache
//
// That works through the environment each uv process gets, from Env, so every
// process must be started with Dirs.Command.
//
// The uv archive is checked against a SHA-256 built into this package, from
// the release's sha256.sum when the version was pinned (see release.go). uv
// checks the Pythons it downloads itself.
package uv

//go:generate go run ./gen -version 0.13.9

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// DefaultPython is the Python Options.Python asks uv for when empty.
const DefaultPython = "3.14t"

// Dirs are the brainctl directory and the directories in it.
type Dirs struct {
	// Root is the brainctl directory.
	Root string
	// Bin holds uv and uvx, and the commands of the Pythons and tools uv
	// installs.
	Bin string
	// Python holds the Pythons uv installs.
	Python string
	// Tools holds the tools uv installs.
	Tools string
	// Cache is uv's cache.
	Cache string
}

// NewDirs returns the directories in the brainctl directory root.
func NewDirs(root string) Dirs {
	return Dirs{
		Root:   root,
		Bin:    filepath.Join(root, "bin"),
		Python: filepath.Join(root, "python"),
		Tools:  filepath.Join(root, "tools"),
		Cache:  filepath.Join(root, "cache"),
	}
}

// DefaultDirs returns the directories in the brainctl directory for this
// user: ~/.local/opt/brainctl, or %LOCALAPPDATA%\brainctl on Windows.
func DefaultDirs() (Dirs, error) {
	root, err := rootDir(os.Getenv("HOME"), os.Getenv("LOCALAPPDATA"), os.Getenv("USERPROFILE"),
		runtime.GOOS == "windows")
	if err != nil {
		return Dirs{}, err
	}
	return NewDirs(root), nil
}

// rootDir is the brainctl directory:
//  1. Windows: %LOCALAPPDATA%\brainctl (falling back to
//     %USERPROFILE%\AppData\Local\brainctl), local rather than roaming since
//     it is large and machine-specific;
//  2. otherwise $HOME/.local/opt/brainctl.
//
// Empty values count as unset.
func rootDir(home, localAppData, userProfile string, windows bool) (string, error) {
	if windows {
		if localAppData != "" {
			return filepath.Join(localAppData, "brainctl"), nil
		}
		if userProfile != "" {
			return filepath.Join(userProfile, "AppData", "Local", "brainctl"), nil
		}
		return "", errors.New("cannot determine the brainctl directory: set LOCALAPPDATA")
	}
	if home == "" {
		return "", errors.New("cannot determine the brainctl directory: set HOME")
	}
	return filepath.Join(home, ".local", "opt", "brainctl"), nil
}

// UV is the path of the uv command.
func (d Dirs) UV() string { return filepath.Join(d.Bin, exe("uv")) }

func exe(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// droppedVars are uv variables of the user's that would make uv look outside
// the brainctl directory, or that conflict with the ones Env sets.
var droppedVars = []string{
	"UV_PYTHON",
	"UV_PYTHON_PREFERENCE",
	"UV_NO_MANAGED_PYTHON",
	"UV_SYSTEM_PYTHON",
	"UV_CONFIG_FILE",
	"UV_TOOL_DIR",
	"UV_TOOL_BIN_DIR",
	"UV_PYTHON_INSTALL_DIR",
	"UV_PYTHON_BIN_DIR",
	"UV_CACHE_DIR",
	"UV_NO_CACHE",
}

// Env returns environ, such as os.Environ(), for a uv process: without the
// user's droppedVars, and with variables that keep uv in the brainctl
// directory. Other variables, such as proxy settings, package indexes and
// UV_SYSTEM_CERTS, are kept.
func (d Dirs) Env(environ []string) []string {
	env := make([]string, 0, len(environ)+8)
	for _, kv := range environ {
		name, _, _ := strings.Cut(kv, "=")
		if !containsFold(droppedVars, name) {
			env = append(env, kv)
		}
	}
	return append(env,
		"UV_PYTHON_INSTALL_DIR="+d.Python,
		"UV_PYTHON_BIN_DIR="+d.Bin,
		"UV_TOOL_DIR="+d.Tools,
		"UV_TOOL_BIN_DIR="+d.Bin,
		"UV_CACHE_DIR="+d.Cache,
		// Only Pythons in the brainctl directory, never the system's or the
		// user's uv's.
		"UV_MANAGED_PYTHON=1",
		// Ignore the user's uv.toml and pyproject.toml settings.
		"UV_NO_CONFIG=1",
	)
}

// containsFold reports whether list holds name, ignoring case: Windows
// variable names are case-insensitive.
func containsFold(list []string, name string) bool {
	for _, s := range list {
		if strings.EqualFold(s, name) {
			return true
		}
	}
	return false
}

// Command returns a command that runs name, such as d.UV(), with args, in the
// environment from Env, with stdout and stderr going to log.
func (d Dirs) Command(log io.Writer, name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.Env = d.Env(os.Environ())
	cmd.Stdout = log
	cmd.Stderr = log
	return cmd
}

// Options says what SetUp installs.
type Options struct {
	// Python is what uv python install is asked for, such as 3.14, 3.14t
	// (free-threaded) or 3.13.9. Empty means DefaultPython.
	Python string
	// Log receives progress, notes and uv's output; nil means os.Stderr.
	// Nothing is ever written to stdout, so callers keep it for their own
	// output.
	Log io.Writer
	// Quiet discards everything that would go to Log.
	Quiet bool
}

func (o Options) python() string {
	if o.Python == "" {
		return DefaultPython
	}
	return o.Python
}

func (o Options) log() io.Writer {
	switch {
	case o.Quiet:
		return io.Discard
	case o.Log != nil:
		return o.Log
	}
	return os.Stderr
}

// Result is what SetUp installed.
type Result struct {
	// Dirs are the directories it installed into.
	Dirs Dirs
	// Python is the path of the Python interpreter.
	Python string
}

// SetUp installs uv into the brainctl directory for this user with Install,
// then Python with InstallPython.
func SetUp(opts Options) (Result, error) {
	d, err := DefaultDirs()
	if err != nil {
		return Result{}, err
	}
	if err := Install(d, opts); err != nil {
		return Result{}, err
	}
	python, err := InstallPython(d, opts)
	if err != nil {
		return Result{}, err
	}
	return Result{Dirs: d, Python: python}, nil
}
