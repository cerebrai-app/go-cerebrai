package main

import (
	"strings"
	"testing"

	"github.com/cerebrai-app/go-cerebrai/internal/buildinfo"
)

func TestVersion(t *testing.T) {
	var stdout, stderr strings.Builder
	if code := run([]string{"version"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code %d, stderr %q", code, stderr.String())
	}
	if got, want := stdout.String(), "brainctl "+buildinfo.Version()+"\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"nope"}, {"version", "extra"}} {
		var stdout, stderr strings.Builder
		if code := run(args, &stdout, &stderr); code != 2 {
			t.Errorf("run(%q) = %d, want 2", args, code)
		}
		if !strings.HasPrefix(stderr.String(), "usage: brainctl") {
			t.Errorf("run(%q) stderr = %q, want usage", args, stderr.String())
		}
	}
}
