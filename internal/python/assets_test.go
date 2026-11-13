package python

import (
	"slices"
	"strings"
	"testing"
)

const lnx = "x86_64-unknown-linux-gnu"

const sampleSums = `aaa  cpython-3.14.9+20261001-x86_64-unknown-linux-gnu-freethreaded+pgo+lto-full.tar.zst
bbb  cpython-3.14.10+20261026-x86_64-unknown-linux-gnu-freethreaded+pgo+lto-full.tar.zst
ccc  cpython-3.14.10+20261026-x86_64-unknown-linux-gnu-pgo+lto-full.tar.zst
ddd  cpython-3.14.10+20261026-x86_64-unknown-linux-gnu-freethreaded-install_only.tar.gz
eee  cpython-3.15.0a1+20261026-x86_64-unknown-linux-gnu-freethreaded+pgo+lto-full.tar.zst
fff  cpython-3.14.10+20261026-aarch64-unknown-linux-gnu-freethreaded+pgo+lto-full.tar.zst
ggg  cpython-3.13.9+20261026-x86_64-unknown-linux-gnu-pgo+lto-full.tar.zst
`

func TestParseSumsKeepsOnlyFullArchivesForTriple(t *testing.T) {
	assets := parseSums(sampleSums, lnx)
	// install_only, aarch64 and the 3.15.0a1 pre-release are skipped.
	if len(assets) != 4 {
		t.Fatalf("got %d assets, want 4: %+v", len(assets), assets)
	}
	a := assets[1]
	if !slices.Equal(a.version, []int{3, 14, 10}) || a.buildOptions != "freethreaded+pgo+lto" || a.sha256 != "bbb" {
		t.Errorf("got %+v", a)
	}
}

func TestSelectPicksNewestPatchForVersionPrefix(t *testing.T) {
	a, ok := selectAsset(parseSums(sampleSums, lnx), "3.14", []string{DefaultVariant})
	if !ok || a.dirName() != "3.14.10+freethreaded+pgo+lto" {
		t.Errorf("got %+v, %v", a, ok)
	}
}

func TestSelectExactVersionAndNoPartialComponentMatch(t *testing.T) {
	assets := parseSums(sampleSums, lnx)
	if a, ok := selectAsset(assets, "3.14.9", []string{DefaultVariant}); !ok || a.sha256 != "aaa" {
		t.Errorf("3.14.9: got %+v, %v", a, ok)
	}
	// "3.1" must not match 3.14.x.
	if a, ok := selectAsset(assets, "3.1", []string{DefaultVariant}); ok {
		t.Errorf("3.1 matched %+v", a)
	}
}

func TestSelectFallsBackInPreferenceOrder(t *testing.T) {
	assets := parseSums(sampleSums, lnx)
	// 3.13 has no free-threaded build in the sample, so pgo+lto is used.
	a, ok := selectAsset(assets, "3.13", []string{DefaultVariant, "freethreaded+pgo", "pgo+lto"})
	if !ok || a.buildOptions != "pgo+lto" {
		t.Errorf("got %+v, %v", a, ok)
	}
	if _, ok := selectAsset(assets, "3.13", []string{DefaultVariant}); ok {
		t.Error("3.13 without fallbacks: want no match")
	}
}

func TestDefaultChainPrefersOptimizationsOverFreethreaded(t *testing.T) {
	want := []string{
		"freethreaded+pgo+lto", "pgo+lto", "freethreaded+pgo", "pgo",
		"freethreaded+lto", "lto", "freethreaded+noopt", "noopt",
	}
	if got := defaultVariants(); !slices.Equal(got, want) {
		t.Errorf("got %q", got)
	}
	// Windows-like: no LTO and a free-threaded PGO build exists. The
	// non-free-threaded pgo+lto would win if present; here only
	// freethreaded+pgo and pgo exist, so freethreaded+pgo is chosen.
	const win = "x86_64-pc-windows-msvc"
	sums := `a  cpython-3.14.10+1-x86_64-pc-windows-msvc-freethreaded+pgo-full.tar.zst
b  cpython-3.14.10+1-x86_64-pc-windows-msvc-pgo-full.tar.zst
c  cpython-3.14.10+1-x86_64-pc-windows-msvc-noopt-full.tar.zst
`
	if a, _ := selectAsset(parseSums(sums, win), "3.14", defaultVariants()); a.buildOptions != "freethreaded+pgo" {
		t.Errorf("got %q, want freethreaded+pgo", a.buildOptions)
	}
	// Without it, plain pgo beats freethreaded+noopt and noopt.
	sums = strings.ReplaceAll(sums, "freethreaded+pgo", "x")
	if a, _ := selectAsset(parseSums(sums, win), "3.14", defaultVariants()); a.buildOptions != "pgo" {
		t.Errorf("got %q, want pgo", a.buildOptions)
	}
}

func TestParseLineRejectsPathSeparators(t *testing.T) {
	if _, ok := parseLine("aaa  cpython-3.14.10+1-x86_64-unknown-linux-gnu-pgo+lto-full.tar.zst", lnx); !ok {
		t.Error("valid line rejected")
	}
	if a, ok := parseLine("aaa *cpython-3.14.10+1-x86_64-unknown-linux-gnu-pgo+lto-full.tar.zst", lnx); !ok || a.fileName[0] != 'c' {
		t.Errorf("binary-mode marker: got %+v, %v", a, ok)
	}
	for _, bad := range []string{
		"aaa  cpython-3.14.10+../../x-x86_64-unknown-linux-gnu-pgo+lto-full.tar.zst",
		"aaa  cpython-3.14.10+1-x86_64-unknown-linux-gnu-a/b-full.tar.zst",
		`aaa  cpython-3.14.10+1-x86_64-unknown-linux-gnu-a\b-full.tar.zst`,
		"aaa",
		"",
	} {
		if a, ok := parseLine(bad, lnx); ok {
			t.Errorf("%q: accepted as %+v", bad, a)
		}
	}
}

func TestParseVersionAcceptsOnlyPlainNumbers(t *testing.T) {
	if v, ok := parseVersion("3.14.10"); !ok || !slices.Equal(v, []int{3, 14, 10}) {
		t.Errorf("3.14.10: got %v, %v", v, ok)
	}
	if v, ok := parseVersion("3"); !ok || !slices.Equal(v, []int{3}) {
		t.Errorf("3: got %v, %v", v, ok)
	}
	for _, bad := range []string{"", "3.", ".3", "3.x", "3.15.0a1", "v3.14", "+3", "-3"} {
		if v, ok := parseVersion(bad); ok {
			t.Errorf("%q: got %v", bad, v)
		}
	}
	if versionMatches([]int{3, 14, 10}, "") {
		t.Error(`"" matched`)
	}
	if versionMatches([]int{3, 14}, "3.14.1") {
		t.Error("a longer want matched a shorter version")
	}
}

func TestPreferredBuildBeatsANewerVersionAndSaysSo(t *testing.T) {
	sums := `a  cpython-3.15.1+1-x86_64-unknown-linux-gnu-pgo+lto-full.tar.zst
b  cpython-3.15.1+1-x86_64-unknown-linux-gnu-pgo-full.tar.zst
c  cpython-3.14.10+1-x86_64-unknown-linux-gnu-freethreaded+pgo+lto-full.tar.zst
d  cpython-3.14.10+1-x86_64-unknown-linux-gnu-pgo+lto-full.tar.zst
`
	assets := parseSums(sums, lnx)

	// "3": 3.15.1 has no free-threaded build, 3.14.10 does, so the preferred
	// build wins and the user is told about the newer version.
	a, _ := selectAsset(assets, "3", defaultVariants())
	if a.dirName() != "3.14.10+freethreaded+pgo+lto" {
		t.Fatalf("3: got %s", a.dirName())
	}
	want := "note: Python 3.15.1 exists only as pgo, pgo+lto; using 3.14.10 with freethreaded+pgo+lto"
	if note := newerVersionNote(assets, "3", a); note != want {
		t.Errorf("note:\n got %q\nwant %q", note, want)
	}

	// "3.14": the newest 3.14.x already has the preferred build: no note.
	a, _ = selectAsset(assets, "3.14", defaultVariants())
	if note := newerVersionNote(assets, "3.14", a); a.dirName() != "3.14.10+freethreaded+pgo+lto" || note != "" {
		t.Errorf("3.14: got %s, note %q", a.dirName(), note)
	}

	// "3.15": only the fallback builds exist, and the newest is chosen: no note.
	a, _ = selectAsset(assets, "3.15", defaultVariants())
	if note := newerVersionNote(assets, "3.15", a); a.dirName() != "3.15.1+pgo+lto" || note != "" {
		t.Errorf("3.15: got %s, note %q", a.dirName(), note)
	}
}

func TestAvailable(t *testing.T) {
	assets := parseSums(sampleSums, lnx)
	if got := available(assets, "3.14"); got != "freethreaded+pgo+lto, pgo+lto" {
		t.Errorf("3.14: got %q", got)
	}
	if got := available(assets, "3.15"); !strings.HasPrefix(got, "none") {
		t.Errorf("3.15: got %q", got)
	}
}
