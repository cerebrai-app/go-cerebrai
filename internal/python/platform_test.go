package python

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHostTriple(t *testing.T) {
	gnu := func() string { return "gnu" }
	musl := func() string { return "musl" }
	for _, tt := range []struct {
		goos, goarch string
		libc         func() string
		want         string
	}{
		{"linux", "amd64", gnu, "x86_64-unknown-linux-gnu"},
		{"linux", "amd64", musl, "x86_64-unknown-linux-musl"},
		{"linux", "arm64", gnu, "aarch64-unknown-linux-gnu"},
		{"linux", "arm64", musl, "aarch64-unknown-linux-musl"},
		{"darwin", "amd64", gnu, "x86_64-apple-darwin"},
		{"darwin", "arm64", gnu, "aarch64-apple-darwin"},
		{"windows", "amd64", gnu, "x86_64-pc-windows-msvc"},
		{"windows", "arm64", gnu, "aarch64-pc-windows-msvc"},
	} {
		if got, err := hostTriple(tt.goos, tt.goarch, tt.libc); err != nil || got != tt.want {
			t.Errorf("%s/%s: got %q, %v, want %q", tt.goos, tt.goarch, got, err, tt.want)
		}
	}
	for _, bad := range [][2]string{{"linux", "386"}, {"freebsd", "amd64"}, {"plan9", "arm64"}} {
		if got, err := hostTriple(bad[0], bad[1], gnu); err == nil {
			t.Errorf("%s/%s: got %q, want an error", bad[0], bad[1], got)
		}
	}
}

func TestLibcFromInterpreter(t *testing.T) {
	for interp, want := range map[string]string{
		"/lib/ld-musl-x86_64.so.1\x00":         "musl",
		"/lib/ld-musl-aarch64.so.1":            "musl",
		"/lib64/ld-linux-x86-64.so.2\x00":      "gnu",
		"/lib/ld-linux-aarch64.so.1":           "gnu",
		"/nix/store/abc-glibc/lib/ld-linux.so": "gnu",
		"":                                     "gnu",
	} {
		if got := libcFromInterpreter(interp); got != want {
			t.Errorf("%q: got %q, want %q", interp, got, want)
		}
	}
}

func TestSystemLibcDoesNotPanic(t *testing.T) {
	if got := systemLibc(); got != "gnu" && got != "musl" {
		t.Errorf("got %q", got)
	}
}

// Paths come from TempDir so they are absolute on every platform (a bare
// "/x/data" has no drive letter on Windows).
func TestDataHomePrefersAbsoluteXDGEverywhere(t *testing.T) {
	xdg := filepath.Join(os.TempDir(), "xdg-data")
	other := filepath.Join(os.TempDir(), "other")
	for _, windows := range []bool{false, true} {
		if got, err := dataHome(xdg, other, other, other, windows); err != nil || got != xdg {
			t.Errorf("windows=%v: got %q, %v", windows, got, err)
		}
	}
}

func TestDataHomeUnixFallsBackToLocalShare(t *testing.T) {
	home := filepath.Join(os.TempDir(), "home-u")
	want := filepath.Join(home, ".local", "share")
	// A relative XDG_DATA_HOME is invalid per the spec and ignored.
	for _, xdg := range []string{"relative", ""} {
		if got, err := dataHome(xdg, home, "", "", false); err != nil || got != want {
			t.Errorf("xdg=%q: got %q, %v", xdg, got, err)
		}
	}
	// No HOME (or an empty one) must not turn into a relative .local/share.
	if got, err := dataHome("", "", "", "", false); err == nil {
		t.Errorf("no HOME: got %q", got)
	}
	if got, err := dataHome("", "", "", home, false); err != nil || got != want {
		t.Errorf("USERPROFILE fallback: got %q, %v", got, err)
	}
}

func TestDataHomeWindowsUsesLocalAppData(t *testing.T) {
	local := filepath.Join(os.TempDir(), "Local")
	profile := filepath.Join(os.TempDir(), "profile")
	if got, _ := dataHome("", "", local, profile, true); got != local {
		t.Errorf("got %q, want %q", got, local)
	}
	// No LOCALAPPDATA: derive it from the user profile.
	if got, _ := dataHome("", "", "", profile, true); got != filepath.Join(profile, "AppData", "Local") {
		t.Errorf("got %q", got)
	}
	// LOCALAPPDATA is ignored off Windows.
	if got, _ := dataHome("", profile, local, "", false); got != filepath.Join(profile, ".local", "share") {
		t.Errorf("got %q", got)
	}
}
