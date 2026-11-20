package uv

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestRootDir(t *testing.T) {
	home := filepath.Join(os.TempDir(), "home")
	local := filepath.Join(os.TempDir(), "local")
	for _, tt := range []struct {
		home, local, profile string
		windows              bool
		want                 string
	}{
		{home, local, home, false, filepath.Join(home, ".local", "opt", "brainctl")},
		{home, local, home, true, filepath.Join(local, "brainctl")},
		{"", "", home, true, filepath.Join(home, "AppData", "Local", "brainctl")},
	} {
		if got, err := rootDir(tt.home, tt.local, tt.profile, tt.windows); err != nil || got != tt.want {
			t.Errorf("%+v: got %q, %v", tt, got, err)
		}
	}
	// No HOME must not turn into a relative .local/opt.
	if got, err := rootDir("", local, home, false); err == nil {
		t.Errorf("no HOME: got %q", got)
	}
	if got, err := rootDir(home, "", "", true); err == nil {
		t.Errorf("no LOCALAPPDATA or USERPROFILE: got %q", got)
	}
}

func TestNewDirs(t *testing.T) {
	root := filepath.Join(os.TempDir(), "brainctl")
	d := NewDirs(root)
	want := Dirs{
		Root:   root,
		Bin:    filepath.Join(root, "bin"),
		Python: filepath.Join(root, "python"),
		Tools:  filepath.Join(root, "tools"),
		Cache:  filepath.Join(root, "cache"),
	}
	if d != want {
		t.Errorf("got %+v", d)
	}
	if d.UV() != filepath.Join(root, "bin", exe("uv")) {
		t.Errorf("UV() = %s", d.UV())
	}
}

// written returns Dirs in a temporary directory, with the environment file
// written.
func written(t *testing.T) Dirs {
	t.Helper()
	d := NewDirs(filepath.Join(t.TempDir(), "brainctl"))
	if err := WriteEnvFile(d); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestEnvKeepsUVInTheBrainctlDirectory(t *testing.T) {
	d := written(t)
	env, err := d.Env([]string{
		"PATH=/bin",
		"HTTPS_PROXY=http://proxy:3128",
		"UV_INDEX_URL=https://pypi.example/simple",
		"UV_SYSTEM_CERTS=1",
		// The user's own settings that would point uv elsewhere.
		"UV_PYTHON=3.12",
		"uv_python_install_dir=/elsewhere", // Windows names ignore case
		"UV_NO_MANAGED_PYTHON=1",
		"UV_CONFIG_FILE=/home/u/uv.toml",
		"UV_CACHE_DIR=/home/u/.cache/uv",
		"UV_TOOL_BIN_DIR=/home/u/.local/bin",
		"UV_PYTHON_DOWNLOADS=never",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"PATH=/bin",
		"HTTPS_PROXY=http://proxy:3128",
		"UV_INDEX_URL=https://pypi.example/simple",
		"UV_SYSTEM_CERTS=1",
		"UV_PYTHON_INSTALL_DIR=" + d.Python,
		"UV_PYTHON_BIN_DIR=" + d.Bin,
		"UV_TOOL_DIR=" + d.Tools,
		"UV_TOOL_BIN_DIR=" + d.Bin,
		"UV_CACHE_DIR=" + d.Cache,
		"UV_MANAGED_PYTHON=1",
		"UV_NO_CONFIG=1",
	}
	if !slices.Equal(env, want) {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(env, "\n"), strings.Join(want, "\n"))
	}
}

// Variables the user adds to the file reach the process, over the ones in
// the process environment.
func TestEnvAddsTheFilesOtherVariables(t *testing.T) {
	d := written(t)
	appendFile(t, d.EnvFile(), "UV_INDEX_URL=https://brainctl.example/simple\nEMPTY=\n")
	env, err := d.Env([]string{"UV_INDEX_URL=https://pypi.example/simple", "HOME=/home/u"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(env, "UV_INDEX_URL=https://brainctl.example/simple") ||
		slices.Contains(env, "UV_INDEX_URL=https://pypi.example/simple") ||
		!slices.Contains(env, "EMPTY=") || !slices.Contains(env, "HOME=/home/u") {
		t.Errorf("env %q", env)
	}
}

func TestEnvNeedsTheFile(t *testing.T) {
	d := NewDirs(t.TempDir())
	if _, err := d.Env(nil); err == nil || !strings.Contains(err.Error(), "run brainctl init first") {
		t.Errorf("no file: %v", err)
	}
	if _, err := d.Command(nil, "uv"); err == nil {
		t.Error("Command without the file")
	}
}

func TestCommandUsesEnv(t *testing.T) {
	t.Setenv("UV_PYTHON", "3.12")
	d := written(t)
	cmd, err := d.Command(nil, "uv", "--version")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(cmd.Env, "UV_CACHE_DIR="+d.Cache) {
		t.Errorf("env %q", cmd.Env)
	}
	for _, kv := range cmd.Env {
		if strings.HasPrefix(kv, "UV_PYTHON=") {
			t.Errorf("kept %s", kv)
		}
	}
}

func TestOptionsDefaults(t *testing.T) {
	if (Options{}).python() != DefaultPython || (Options{Python: "3.13"}).python() != "3.13" {
		t.Error("python()")
	}
	if (Options{}).log() != os.Stderr {
		t.Error("log() defaults to stderr")
	}
}
