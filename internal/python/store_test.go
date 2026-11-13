package python

import (
	"archive/tar"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)

// tarEntry is one entry of a test archive.
type tarEntry struct {
	name     string
	typeflag byte
	body     string
	linkname string
	mode     int64
}

// makeArchive builds a .tar.zst holding entries.
func makeArchive(t *testing.T, entries ...tarEntry) []byte {
	t.Helper()
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	for _, e := range entries {
		hdr := &tar.Header{
			Name: e.name, Typeflag: e.typeflag, Linkname: e.linkname,
			Mode: e.mode, Size: int64(len(e.body)), ModTime: time.Unix(1_700_000_000, 0),
		}
		if hdr.Mode == 0 {
			hdr.Mode = 0o644
		}
		if hdr.Typeflag == 0 {
			hdr.Typeflag = tar.TypeReg
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(e.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	enc, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = enc.Close() }()
	return enc.EncodeAll(tarBuf.Bytes(), nil)
}

// pythonArchive is a tiny archive shaped like the real ones: python/PYTHON.json.
func pythonArchive(t *testing.T) []byte {
	return makeArchive(t, tarEntry{name: "python/PYTHON.json", body: "hi"})
}

func writeTestFile(t *testing.T, name string, data []byte) {
	t.Helper()
	if err := os.WriteFile(name, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestUnpackAndPublishInstallsOnceAndReportsLostRace(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "a.tar.zst")
	writeTestFile(t, archive, pythonArchive(t))

	dest := filepath.Join(dir, "3.14.1+x")
	partial := filepath.Join(dir, "3.14.1+x.1.partial")
	if ok, err := unpackAndPublish(archive, partial, dest); !ok || err != nil {
		t.Fatalf("first install: %v, %v", ok, err)
	}
	if got := readFile(t, filepath.Join(dest, "python", "PYTHON.json")); got != "hi" {
		t.Errorf("got %q", got)
	}
	if exists(partial) {
		t.Error("partial dir is still there")
	}

	// Second run: dest now exists, so this one lost the race.
	if ok, err := unpackAndPublish(archive, filepath.Join(dir, "3.14.1+x.2.partial"), dest); ok || err != nil {
		t.Errorf("lost race: got %v, %v, want false, nil", ok, err)
	}
	if got := readFile(t, filepath.Join(dest, "python", "PYTHON.json")); got != "hi" {
		t.Errorf("after lost race: got %q", got)
	}

	// A corrupt archive is an error and installs nothing.
	bad := filepath.Join(dir, "bad.tar.zst")
	writeTestFile(t, bad, []byte("not a zstd file"))
	dest2 := filepath.Join(dir, "3.14.2+x")
	if _, err := unpackAndPublish(bad, filepath.Join(dir, "3.14.2+x.3.partial"), dest2); err == nil {
		t.Error("corrupt archive: no error")
	}
	if exists(dest2) {
		t.Error("corrupt archive installed something")
	}
}

func TestUnpackKeepsModesAndLinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes and symlinks need Unix")
	}
	dir := t.TempDir()
	archive := filepath.Join(dir, "a.tar.zst")
	writeTestFile(t, archive, makeArchive(t,
		tarEntry{name: "python/", typeflag: tar.TypeDir, mode: 0o755},
		tarEntry{name: "python/bin/python3.14", body: "#!", mode: 0o755},
		tarEntry{name: "python/bin/python3", typeflag: tar.TypeSymlink, linkname: "python3.14"},
		tarEntry{name: "python/bin/python", typeflag: tar.TypeLink, linkname: "python/bin/python3.14"},
		tarEntry{name: "python/lib/readonly", typeflag: tar.TypeDir, mode: 0o555},
		tarEntry{name: "python/lib/readonly/x.py", body: "x"},
	))
	out := filepath.Join(dir, "out")
	if err := unpack(archive, out); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(out, "python", "lib", "readonly"), 0o755) })

	exe := filepath.Join(out, "python", "bin", "python3.14")
	if info, err := os.Stat(exe); err != nil || info.Mode().Perm() != 0o755 {
		t.Errorf("executable: %v, %v", info, err)
	} else if !info.ModTime().Equal(time.Unix(1_700_000_000, 0)) {
		t.Errorf("mtime %v", info.ModTime())
	}
	if target, err := os.Readlink(filepath.Join(out, "python", "bin", "python3")); err != nil || target != "python3.14" {
		t.Errorf("symlink: %q, %v", target, err)
	}
	if got := readFile(t, filepath.Join(out, "python", "bin", "python")); got != "#!" {
		t.Errorf("hard link: %q", got)
	}
	if info, err := os.Stat(filepath.Join(out, "python", "lib", "readonly")); err != nil || info.Mode().Perm() != 0o555 {
		t.Errorf("read-only dir: %v, %v", info, err)
	}
}

func TestRenameRetryingMovesRetriesAndGivesUp(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatal(err)
	}

	// Plain success.
	dest := filepath.Join(dir, "dest")
	if err := renameRetrying(src, dest, 3, 0); err != nil {
		t.Fatal(err)
	}
	if exists(src) || !exists(dest) {
		t.Error("not moved")
	}

	// Destination exists (lost race): the error comes back at once, with no
	// sleeping, so the caller can report "already installed".
	for _, d := range []string{filepath.Join(dir, "src2", "sub"), filepath.Join(dest, "sub")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	start := time.Now()
	if err := renameRetrying(filepath.Join(dir, "src2"), dest, 5, 5*time.Second); err == nil {
		t.Error("rename onto a non-empty dir succeeded")
	}
	if time.Since(start) > 2*time.Second {
		t.Error("retried although the destination exists")
	}

	// Still failing and the destination never appears: tries every attempt
	// (two sleeps for three attempts), then returns the error.
	start = time.Now()
	if err := renameRetrying(filepath.Join(dir, "missing"), filepath.Join(dir, "never"), 3, 60*time.Millisecond); err == nil {
		t.Error("renaming a missing dir succeeded")
	}
	if time.Since(start) < 120*time.Millisecond {
		t.Error("did not retry")
	}
}

func TestUnpackRefusesEntriesThatEscapeTheInstallDir(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(dir, "outside")
	work := filepath.Join(dir, "work")
	for _, d := range []string{outside, work} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cases := map[string][]tarEntry{
		"dot-dot":  {{name: "../evil.txt", body: "evil"}},
		"absolute": {{name: filepath.ToSlash(filepath.Join(outside, "abs.txt")), body: "evil"}},
		"symlink": {
			{name: "link", typeflag: tar.TypeSymlink, linkname: outside},
			{name: "link/pwned.txt", body: "evil"},
		},
		"relative symlink": {
			{name: "up", typeflag: tar.TypeSymlink, linkname: "../../outside"},
			{name: "up/pwned2.txt", body: "evil"},
		},
		"hard link": {{name: "hard", typeflag: tar.TypeLink, linkname: "../outside/target"}},
	}
	writeTestFile(t, filepath.Join(outside, "target"), []byte("secret"))
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			archive := filepath.Join(dir, strings.ReplaceAll(name, " ", "-")+".tar.zst")
			writeTestFile(t, archive, makeArchive(t, entries...))
			// The result (an error, or a skipped entry) does not matter. What
			// matters is that nothing was written outside the unpack directory.
			partial := filepath.Join(work, "x.partial")
			_, _ = unpackAndPublish(archive, partial, filepath.Join(work, "dest-"+name))
			_ = os.RemoveAll(partial)
			for _, f := range []string{
				filepath.Join(work, "evil.txt"), filepath.Join(dir, "evil.txt"),
				filepath.Join(outside, "abs.txt"), filepath.Join(outside, "pwned.txt"),
				filepath.Join(outside, "pwned2.txt"),
			} {
				if exists(f) {
					t.Errorf("%s escaped to %s", name, f)
				}
			}
			if readFile(t, filepath.Join(outside, "target")) != "secret" {
				t.Error("file outside was changed")
			}
		})
	}
}

func TestUniqueSuffixDiffersBetweenCalls(t *testing.T) {
	a, b := uniqueSuffix(), uniqueSuffix()
	if !strings.HasPrefix(a, fmt.Sprintf("%d-", os.Getpid())) {
		t.Errorf("got %q", a)
	}
	if a == b {
		t.Errorf("both %q", a)
	}
}

func TestSweepRemovesOnlyOldTempEntries(t *testing.T) {
	root := t.TempDir()
	partial := filepath.Join(root, "3.14.1+x.99.partial")
	download := filepath.Join(root, "3.14.1+x.99.download")
	installed := filepath.Join(root, "3.14.1+installed")
	for _, d := range []string{partial, installed} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFile(t, download, []byte("x"))

	// Nothing is older than a day yet.
	sweepStaleTemps(root, staleTempAge)
	if !exists(partial) || !exists(download) {
		t.Error("removed recent temp entries")
	}

	// With a zero max age, temp entries go but real installs stay.
	sweepStaleTemps(root, 0)
	if exists(partial) || exists(download) {
		t.Error("kept old temp entries")
	}
	if !exists(installed) {
		t.Error("removed an install")
	}
}
