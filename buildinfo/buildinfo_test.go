package buildinfo

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
)

func TestFromBuildInfo(t *testing.T) {
	tests := []struct {
		name string
		info *debug.BuildInfo
		ok   bool
		want string
	}{
		{"no build info", nil, false, "dev"},
		{"devel", &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, true, "dev"},
		{"empty", &debug.BuildInfo{}, true, "dev"},
		{"go install @version", &debug.BuildInfo{Main: debug.Module{Version: "v1.2.3"}}, true, "1.2.3"},
		{
			"vcs pseudo-version",
			&debug.BuildInfo{Main: debug.Module{Version: "v0.1.1-0.20261113120000-0123456789ab+dirty"}},
			true,
			"0.1.1-0.20261113120000-0123456789ab+dirty",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fromBuildInfo(tt.info, tt.ok); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestVersionPrefersLinkedVersion(t *testing.T) {
	old := version
	t.Cleanup(func() { version = old })
	version = "1.2.3"
	if got := Version(); got != "1.2.3" {
		t.Errorf("got %q, want %q", got, "1.2.3")
	}
}

// TestLinkerFlag builds a program the way the release workflow does and checks
// that the version reaches it, so renaming the variable fails here rather than
// in a release.
func TestLinkerFlag(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a program")
	}
	bin := filepath.Join(t.TempDir(), "printversion")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	build := exec.Command("go", "build", "-o", bin,
		"-ldflags", "-X github.com/cerebrai-app/go-cerebrai/buildinfo.version=9.8.7",
		"./testdata/printversion")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got != "9.8.7" {
		t.Errorf("got %q, want %q", got, "9.8.7")
	}
}
