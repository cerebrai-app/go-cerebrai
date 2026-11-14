package brainconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDirDefaultsToCurrentDirectory(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	for _, value := range []string{"", "unset"} {
		if value == "unset" {
			// t.Setenv restores the variable afterwards; unset it within that.
			t.Setenv(EnvVar, "")
			if err := os.Unsetenv(EnvVar); err != nil {
				t.Fatal(err)
			}
		} else {
			t.Setenv(EnvVar, value)
		}
		got, err := Dir()
		if err != nil {
			t.Fatalf("%s=%q: %v", EnvVar, value, err)
		}
		if same, _ := sameDir(got, cwd); !same {
			t.Errorf("%s=%q: Dir = %q, want the current directory %q", EnvVar, value, got, cwd)
		}
	}
}

func TestDirFromEnvironment(t *testing.T) {
	brain := t.TempDir()
	t.Setenv(EnvVar, brain)
	t.Chdir(t.TempDir())
	if got, err := Dir(); err != nil || got != brain {
		t.Errorf("Dir = %q, %v; want %q", got, err, brain)
	}
}

func TestDirRelative(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	if err := os.Mkdir("notes", 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvVar, "notes")
	got, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("Dir = %q, not absolute", got)
	}
	if same, _ := sameDir(got, filepath.Join(cwd, "notes")); !same {
		t.Errorf("Dir = %q, want %q", got, filepath.Join(cwd, "notes"))
	}
}

func TestDirMustExist(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "typo")
	for dir, wantErr := range map[string]string{
		missing: EnvVar + "=" + missing,
		file:    EnvVar + "=" + file + ": not a directory",
	} {
		t.Setenv(EnvVar, dir)
		if got, err := Dir(); err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Errorf("%s=%s: Dir = %q, %v; want an error containing %q", EnvVar, dir, got, err, wantErr)
		}
	}
	if _, err := os.Stat(missing); err == nil {
		t.Error("Dir made the missing directory")
	}
}

func TestDirAndPath(t *testing.T) {
	brain := t.TempDir()
	t.Setenv(EnvVar, brain)
	dir, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := Path(dir), filepath.Join(brain, ".brain", "config.yaml"); got != want {
		t.Errorf("Path = %q, want %q", got, want)
	}
}

// sameDir reports whether a and b are the same directory, allowing for
// symbolic links in temporary directory paths (as on macOS).
func sameDir(a, b string) (bool, error) {
	ai, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	bi, err := os.Stat(b)
	if err != nil {
		return false, err
	}
	return os.SameFile(ai, bi), nil
}
