package uv

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const (
	linuxTriple   = "x86_64-unknown-linux-gnu"
	windowsTriple = "x86_64-pc-windows-msvc"
)

// tarGz returns a .tar.gz holding files, by path, like uv's archives, which
// put them in a uv-<triple>/ directory.
func tarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "uv-" + linuxTriple + "/", Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func zipArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// serve serves archive as Version's release archive for triple, with
// archiveSHA256 set to its hash, and returns the source and a counter of the
// requests made.
func serve(t *testing.T, triple string, archive []byte) (source, *int) {
	t.Helper()
	h := sha256.Sum256(archive)
	old := archiveSHA256[triple]
	archiveSHA256[triple] = hex.EncodeToString(h[:])
	t.Cleanup(func() { archiveSHA256[triple] = old })
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/"+Version+"/"+archiveName(triple) {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(archive)
	}))
	t.Cleanup(srv.Close)
	return source{client: srv.Client(), downloadURL: srv.URL}, &requests
}

// stubVersion makes installedVersion report v.
func stubVersion(t *testing.T, v string) {
	t.Helper()
	old := installedVersion
	t.Cleanup(func() { installedVersion = old })
	installedVersion = func(Dirs) string { return v }
}

func readFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestInstallUnpacksTheBinaries(t *testing.T) {
	stubVersion(t, "")
	for _, tc := range []struct {
		triple  string
		archive []byte
		want    map[string]string
	}{
		{linuxTriple, tarGz(t, map[string]string{
			"uv-" + linuxTriple + "/uv":  "uv binary",
			"uv-" + linuxTriple + "/uvx": "uvx binary",
		}), map[string]string{"uv": "uv binary", "uvx": "uvx binary"}},
		{windowsTriple, zipArchive(t, map[string]string{
			"uv.exe": "uv binary", "uvx.exe": "uvx binary", "uvw.exe": "uvw binary",
		}), map[string]string{"uv.exe": "uv binary", "uvx.exe": "uvx binary", "uvw.exe": "uvw binary"}},
	} {
		src, _ := serve(t, tc.triple, tc.archive)
		d := NewDirs(filepath.Join(t.TempDir(), "brainctl"))
		var log bytes.Buffer
		if err := install(d, tc.triple, src, Options{Log: &log}); err != nil {
			t.Fatalf("%s: %v", tc.triple, err)
		}
		for name, body := range tc.want {
			if got := readFile(t, filepath.Join(d.Bin, name)); got != body {
				t.Errorf("%s: %s holds %q", tc.triple, name, got)
			}
			if info, err := os.Stat(filepath.Join(d.Bin, name)); runtime.GOOS != "windows" && (err != nil || info.Mode().Perm()&0o111 == 0) {
				t.Errorf("%s: %s is not executable: %v", tc.triple, name, info.Mode())
			}
		}
		entries, _ := os.ReadDir(d.Bin)
		if len(entries) != len(tc.want) {
			t.Errorf("%s: bin holds %d entries", tc.triple, len(entries))
		}
		root, _ := os.ReadDir(d.Root)
		for _, e := range root {
			if e.Name() != "bin" {
				t.Errorf("%s: left %s in the brainctl directory", tc.triple, e.Name())
			}
		}
		if !strings.Contains(log.String(), "installed uv "+Version) {
			t.Errorf("%s: log %q", tc.triple, log.String())
		}
	}
}

func TestInstallSkipsTheInstalledVersion(t *testing.T) {
	stubVersion(t, Version)
	src, requests := serve(t, linuxTriple, nil)
	d := NewDirs(t.TempDir())
	var log bytes.Buffer
	if err := install(d, linuxTriple, src, Options{Log: &log}); err != nil {
		t.Fatal(err)
	}
	if *requests != 0 || !strings.Contains(log.String(), "already installed") {
		t.Errorf("%d requests, log %q", *requests, log.String())
	}
}

func TestInstallReplacesAnotherVersion(t *testing.T) {
	stubVersion(t, "0.0.1")
	src, _ := serve(t, linuxTriple, tarGz(t, map[string]string{"x/uv": "new uv", "x/uvx": "new uvx"}))
	d := NewDirs(t.TempDir())
	if err := os.MkdirAll(d.Bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d.Bin, "uv"), []byte("old uv"), 0o755); err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	if err := install(d, linuxTriple, src, Options{Log: &log}); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(d.Bin, "uv")); got != "new uv" {
		t.Errorf("uv holds %q", got)
	}
	if !strings.Contains(log.String(), "replacing uv 0.0.1 with uv "+Version) {
		t.Errorf("log %q", log.String())
	}
}

func TestInstallErrors(t *testing.T) {
	stubVersion(t, "")
	d := NewDirs(t.TempDir())

	if err := install(d, "x86_64-unknown-freebsd", source{}, Options{Quiet: true}); err == nil ||
		!strings.Contains(err.Error(), "no release for") {
		t.Errorf("unknown platform: %v", err)
	}

	src, _ := serve(t, linuxTriple, tarGz(t, map[string]string{"x/uv": "uv"}))
	if err := install(d, linuxTriple, src, Options{Quiet: true}); err == nil ||
		!strings.Contains(err.Error(), "no uvx in the archive") {
		t.Errorf("missing uvx: %v", err)
	}
	if _, err := os.Stat(filepath.Join(d.Bin, "uv")); err == nil {
		t.Error("uv written although the archive lacked uvx")
	}

	src, _ = serve(t, linuxTriple, tarGz(t, map[string]string{"x/uv": "uv", "x/uvx": "uvx"}))
	archiveSHA256[linuxTriple] = strings.Repeat("0", 64)
	if err := install(d, linuxTriple, src, Options{Quiet: true}); err == nil ||
		!strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("bad checksum: %v", err)
	}
	if _, err := os.Stat(filepath.Join(d.Bin, "uv")); err == nil {
		t.Error("uv written although its checksum was wrong")
	}
	entries, _ := os.ReadDir(d.Root)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".download") {
			t.Errorf("left %s", e.Name())
		}
	}
}

// Archive paths only pick the file by its base name: nothing is written
// outside the bin directory, whatever the archive says.
func TestInstallIgnoresArchivePaths(t *testing.T) {
	stubVersion(t, "")
	src, _ := serve(t, linuxTriple, tarGz(t, map[string]string{
		"../../../uv": "uv", "/abs/uvx": "uvx", "x/other": "ignored",
	}))
	dir := t.TempDir()
	d := NewDirs(filepath.Join(dir, "brainctl"))
	if err := install(d, linuxTriple, src, Options{Quiet: true}); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "brainctl" {
		t.Errorf("wrote outside the brainctl directory: %v", entries)
	}
	if _, err := os.Stat(filepath.Join(d.Bin, "other")); err == nil {
		t.Error("extracted a file that isn't uv's")
	}
}
