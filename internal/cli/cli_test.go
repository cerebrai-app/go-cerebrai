package cli

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cerebrai-app/go-cerebrai/internal/buildinfo"
	"github.com/cerebrai-app/go-cerebrai/internal/python"
)

func run(args ...string) (code int, stdout, stderr string) {
	var out, errOut strings.Builder
	code = Main("brainctl", args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestVersion(t *testing.T) {
	code, stdout, stderr := run("version")
	if code != 0 || stdout != "brainctl "+buildinfo.Version()+"\n" || stderr != "" {
		t.Errorf("got %d, %q, %q", code, stdout, stderr)
	}
}

func TestUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		nil, {"nope"}, {"version", "extra"}, {"init", "extra"}, {"init", "--no-such-flag"},
	} {
		code, stdout, stderr := run(args...)
		if code != 2 || stdout != "" || !strings.Contains(stderr, "usage: brainctl") {
			t.Errorf("%q: got %d, %q, %q", args, code, stdout, stderr)
		}
	}
	for _, args := range [][]string{{"help"}, {"--help"}, {"init", "-h"}} {
		if code, _, stderr := run(args...); code != 0 || !strings.Contains(stderr, "usage: brainctl") {
			t.Errorf("%q: got %d, %q", args, code, stderr)
		}
	}
}

// stubInstall replaces install and runs the test in an empty current
// directory, so init's config file goes there.
func stubInstall(t *testing.T, err error) *python.Options {
	t.Helper()
	t.Chdir(t.TempDir())
	var got python.Options
	old := install
	t.Cleanup(func() { install = old })
	install = func(opts python.Options) (string, error) {
		got = opts
		return "/data/python-build-standalone/x", err
	}
	return &got
}

func TestInitPassesOptions(t *testing.T) {
	got := stubInstall(t, nil)
	if code, stdout, stderr := run("init"); code != 0 || stdout != "" {
		t.Fatalf("got %d, %q, %q", code, stdout, stderr)
	}
	if got.PythonVersion != python.DefaultPythonVersion || got.Variant != "" || got.Log == nil {
		t.Errorf("defaults: %+v", *got)
	}
	for _, args := range [][]string{
		{"init", "--python-version", "3.13.9", "--variant", "pgo+lto"},
		{"init", "--python-version=3.13.9", "-variant=pgo+lto"},
	} {
		if code, _, stderr := run(args...); code != 0 {
			t.Fatalf("%q: got %d, %q", args, code, stderr)
		}
		if got.PythonVersion != "3.13.9" || got.Variant != "pgo+lto" {
			t.Errorf("%q: %+v", args, *got)
		}
	}
}

func TestInitReportsInstallErrors(t *testing.T) {
	stubInstall(t, errors.New("no network"))
	code, stdout, stderr := run("init")
	if code != 1 || stdout != "" || !strings.HasSuffix(stderr, "\nerror: no network\n") {
		t.Errorf("got %d, %q, %q", code, stdout, stderr)
	}
}

func TestInitWritesConfig(t *testing.T) {
	stubInstall(t, nil)
	code, _, stderr := run("init")
	if code != 0 || !strings.Contains(stderr, "Wrote ") {
		t.Fatalf("got %d, %q", code, stderr)
	}
	data, err := os.ReadFile(filepath.Join(".brain", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "schema_version: 1\n" {
		t.Errorf("config = %q", data)
	}
	// A second run keeps the file.
	if code, _, stderr := run("init"); code != 0 || !strings.Contains(stderr, "Using existing ") {
		t.Errorf("second run: got %d, %q", code, stderr)
	}
}

func runWorkctl(args ...string) (code int, stderr string) {
	var out, errOut strings.Builder
	code = Main("workctl", args, &out, &errOut)
	return code, errOut.String()
}

func TestWorkctlInitAddsExtension(t *testing.T) {
	stubInstall(t, nil)
	if code, _, stderr := run("init"); code != 0 {
		t.Fatalf("brainctl init: got %d, %q", code, stderr)
	}
	code, stderr := runWorkctl("init")
	if code != 0 || strings.Contains(stderr, "Wrote ") || !strings.Contains(stderr, "Added extensions.workctl to ") {
		t.Fatalf("workctl init: got %d, %q", code, stderr)
	}
	path := filepath.Join(".brain", "config.yaml")
	const want = "schema_version: 1\nextensions:\n  workctl:\n    schema_version: 1\n"
	if data, err := os.ReadFile(path); err != nil || string(data) != want {
		t.Errorf("config = %q, %v; want %q", data, err, want)
	}
	if _, err := os.Stat(".work"); err == nil {
		t.Error("workctl init made a .work directory")
	}
	// Running either init again changes nothing.
	if code, stderr := runWorkctl("init"); code != 0 || !strings.Contains(stderr, "Using existing extensions.workctl") {
		t.Errorf("second workctl init: got %d, %q", code, stderr)
	}
	if code, _, stderr := run("init"); code != 0 {
		t.Errorf("second brainctl init: got %d, %q", code, stderr)
	}
	if data, _ := os.ReadFile(path); string(data) != want {
		t.Errorf("config changed to %q", data)
	}
}

func TestWorkctlInitWithoutBrainConfig(t *testing.T) {
	stubInstall(t, nil)
	code, stderr := runWorkctl("init")
	if code != 0 || !strings.Contains(stderr, "Wrote ") || !strings.Contains(stderr, "Added extensions.workctl") {
		t.Fatalf("got %d, %q", code, stderr)
	}
	const want = "schema_version: 1\nextensions:\n  workctl:\n    schema_version: 1\n"
	if data, err := os.ReadFile(filepath.Join(".brain", "config.yaml")); err != nil || string(data) != want {
		t.Errorf("config = %q, %v; want %q", data, err, want)
	}
}

func TestInitStopsOnBadConfig(t *testing.T) {
	stubInstall(t, nil)
	installed := false
	install = func(python.Options) (string, error) { installed = true; return "", nil }
	if err := os.Mkdir(".brain", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(".brain", "config.yaml"), []byte("schema_version: 99\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := run("init"); code != 1 || !strings.HasPrefix(stderr, "error: ") || installed {
		t.Errorf("got %d, %q, installed %v", code, stderr, installed)
	}
}

func TestVersionWriteError(t *testing.T) {
	var errOut strings.Builder
	if code := Main("brainctl", []string{"version"}, failingWriter{}, &errOut); code != 1 {
		t.Errorf("got %d, stderr %q", code, errOut.String())
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestUnknownExtension(t *testing.T) {
	stubInstall(t, nil)
	var errOut strings.Builder
	if code := Main("nopectl", []string{"init"}, io.Discard, &errOut); code != 1 || !strings.Contains(errOut.String(), "not a known extension") {
		t.Errorf("got %d, %q", code, errOut.String())
	}
}
