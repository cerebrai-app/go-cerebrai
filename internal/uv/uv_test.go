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

func TestEnvKeepsUVInTheBrainctlDirectory(t *testing.T) {
	d := NewDirs(filepath.Join(os.TempDir(), "brainctl"))
	env := d.Env([]string{
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
	})
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

func TestCommandUsesEnv(t *testing.T) {
	t.Setenv("UV_PYTHON", "3.12")
	d := NewDirs(filepath.Join(os.TempDir(), "brainctl"))
	cmd := d.Command(nil, "uv", "--version")
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
