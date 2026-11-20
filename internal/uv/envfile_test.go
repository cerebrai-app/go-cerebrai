package uv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func appendFile(t *testing.T, name, text string) {
	t.Helper()
	f, err := os.OpenFile(name, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(text); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestWriteEnvFileCreatesIt(t *testing.T) {
	d := NewDirs(filepath.Join(t.TempDir(), "brainctl"))
	if err := WriteEnvFile(d); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, d.EnvFile())
	want := envFileHeader +
		"UV_PYTHON_INSTALL_DIR=" + d.Python + "\n" +
		"UV_PYTHON_BIN_DIR=" + d.Bin + "\n" +
		"UV_TOOL_DIR=" + d.Tools + "\n" +
		"UV_TOOL_BIN_DIR=" + d.Bin + "\n" +
		"UV_CACHE_DIR=" + d.Cache + "\n" +
		"UV_MANAGED_PYTHON=1\n" +
		"UV_NO_CONFIG=1\n"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	// Writing it again changes nothing.
	if err := WriteEnvFile(d); err != nil {
		t.Fatal(err)
	}
	if again := readFile(t, d.EnvFile()); again != want {
		t.Errorf("second write:\n%s", again)
	}
	entries, _ := os.ReadDir(d.Root)
	if len(entries) != 1 {
		t.Errorf("left %d entries in the brainctl directory", len(entries))
	}
}

// The required variables are put back, in place; the user's lines are kept.
func TestWriteEnvFileUpdatesIt(t *testing.T) {
	d := NewDirs(t.TempDir())
	old := "# my notes\r\n" +
		"UV_CACHE_DIR=/home/u/.cache/uv\r\n" +
		"UV_INDEX_URL=https://pypi.example/simple\n" +
		"\n" +
		"uv_no_config=0\n" +
		"UV_CACHE_DIR=/again\n"
	if err := os.WriteFile(d.EnvFile(), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteEnvFile(d); err != nil {
		t.Fatal(err)
	}
	want := "# my notes\n" +
		"UV_CACHE_DIR=" + d.Cache + "\n" +
		"UV_INDEX_URL=https://pypi.example/simple\n" +
		"\n" +
		"UV_NO_CONFIG=1\n" +
		"UV_PYTHON_INSTALL_DIR=" + d.Python + "\n" +
		"UV_PYTHON_BIN_DIR=" + d.Bin + "\n" +
		"UV_TOOL_DIR=" + d.Tools + "\n" +
		"UV_TOOL_BIN_DIR=" + d.Bin + "\n" +
		"UV_MANAGED_PYTHON=1\n"
	if got := readFile(t, d.EnvFile()); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestEnvFileErrors(t *testing.T) {
	for _, bad := range []string{"no equals sign\n", "=value\n", "1ABC=x\n", "A B=x\n", "export A=x\n"} {
		d := NewDirs(t.TempDir())
		if err := os.WriteFile(d.EnvFile(), []byte("# ok\n"+bad), 0o644); err != nil {
			t.Fatal(err)
		}
		// A broken file is left for the user to fix, not overwritten.
		if err := WriteEnvFile(d); err == nil || !strings.Contains(err.Error(), d.EnvFile()+":2:") {
			t.Errorf("%q: WriteEnvFile: %v", bad, err)
		}
		if got := readFile(t, d.EnvFile()); got != "# ok\n"+bad {
			t.Errorf("%q: file changed to %q", bad, got)
		}
		if _, err := d.Env(nil); err == nil {
			t.Errorf("%q: Env accepted it", bad)
		}
	}

	// A required variable deleted by hand.
	d := written(t)
	data := strings.ReplaceAll(readFile(t, d.EnvFile()), "UV_NO_CONFIG=1\n", "")
	if err := os.WriteFile(d.EnvFile(), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Env(nil); err == nil || !strings.Contains(err.Error(), "has no UV_NO_CONFIG") {
		t.Errorf("missing variable: %v", err)
	}
}

func TestEnvLine(t *testing.T) {
	for line, want := range map[string]bool{
		"A=1": true, "_a9=x=y": true, "A=": true, "a b=1": false, "9A=1": false, "=1": false, "A": false,
	} {
		if _, _, ok := envLine(line); ok != want {
			t.Errorf("%q: got %v", line, ok)
		}
	}
}
