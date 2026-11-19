package uv

import (
	"errors"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// stubRunUV replaces runUV: python find prints found, and the call whose
// second argument is failOn fails. It returns the calls made.
func stubRunUV(t *testing.T, found, failOn string) *[][]string {
	t.Helper()
	var calls [][]string
	old := runUV
	t.Cleanup(func() { runUV = old })
	runUV = func(_ Dirs, _ io.Writer, capture bool, args ...string) (string, error) {
		calls = append(calls, args)
		if len(args) > 1 && args[1] == failOn {
			return "", errors.New("exit status 2")
		}
		if capture {
			return found + "\n", nil
		}
		return "", nil
	}
	return &calls
}

func TestInstallPython(t *testing.T) {
	d := NewDirs(t.TempDir())
	want := filepath.Join(d.Python, "cpython-3.14", "bin", "python3.14t")
	calls := stubRunUV(t, want, "")
	got, err := InstallPython(d, Options{Quiet: true})
	if err != nil || got != want {
		t.Fatalf("got %q, %v", got, err)
	}
	if !slices.EqualFunc(*calls, [][]string{
		{"python", "install", "--no-registry", DefaultPython},
		{"python", "find", DefaultPython},
	}, slices.Equal) {
		t.Errorf("calls %q", *calls)
	}

	calls = stubRunUV(t, want, "")
	if _, err := InstallPython(d, Options{Python: "3.13.9", Quiet: true}); err != nil {
		t.Fatal(err)
	}
	if (*calls)[0][3] != "3.13.9" || (*calls)[1][2] != "3.13.9" {
		t.Errorf("calls %q", *calls)
	}
}

func TestInstallPythonErrors(t *testing.T) {
	d := NewDirs(t.TempDir())
	for _, bad := range []string{"--no-cache", "-3", " 3.14", "3.14\n"} {
		calls := stubRunUV(t, "/python", "")
		if _, err := InstallPython(d, Options{Python: bad, Quiet: true}); err == nil ||
			!strings.Contains(err.Error(), "invalid Python version") {
			t.Errorf("%q: %v", bad, err)
		}
		if len(*calls) != 0 {
			t.Errorf("%q: ran uv", bad)
		}
	}
	for _, step := range []string{"install", "find"} {
		stubRunUV(t, "/python", step)
		if _, err := InstallPython(d, Options{Quiet: true}); err == nil ||
			!strings.Contains(err.Error(), "uv python "+step) {
			t.Errorf("%s failing: %v", step, err)
		}
	}
	stubRunUV(t, "", "")
	if _, err := InstallPython(d, Options{Quiet: true}); err == nil {
		t.Error("empty find output accepted")
	}
}
