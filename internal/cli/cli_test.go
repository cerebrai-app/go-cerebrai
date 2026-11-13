package cli

import (
	"errors"
	"io"
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

func stubInstall(t *testing.T, err error) *python.Options {
	t.Helper()
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
	if code != 1 || stdout != "" || stderr != "error: no network\n" {
		t.Errorf("got %d, %q, %q", code, stdout, stderr)
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
