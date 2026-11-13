package python

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

const (
	testTriple = "x86_64-unknown-linux-gnu"
	testTag    = "20990101"
)

// release serves the files of one release, Python 3.14.1
// freethreaded+pgo+lto, whose SHA256SUMS entry is sum (the real hash when
// empty). It returns the server and the URL path of the archive.
func release(t *testing.T, sum string) (*releaseServer, string) {
	t.Helper()
	body := pythonArchive(t)
	if sum == "" {
		h := sha256.Sum256(body)
		sum = hex.EncodeToString(h[:])
	}
	name := "cpython-3.14.1+" + testTag + "-" + testTriple + "-freethreaded+pgo+lto-full.tar.zst"
	archivePath := "/download/" + testTag + "/" + strings.ReplaceAll(name, "+", "%2B")
	s := &releaseServer{files: map[string][]byte{
		"/latest-release.json":                 []byte(`{"version":1,"tag":"` + testTag + `"}`),
		"/download/" + testTag + "/SHA256SUMS": []byte(sum + "  " + name + "\n"),
		archivePath:                            body,
	}}
	s.srv = httptest.NewServer(s)
	t.Cleanup(s.srv.Close)
	return s, archivePath
}

type releaseServer struct {
	srv   *httptest.Server
	files map[string][]byte

	mu       sync.Mutex
	requests []string
}

func (s *releaseServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.requests = append(s.requests, r.URL.EscapedPath())
	s.mu.Unlock()
	body, ok := s.files[r.URL.EscapedPath()]
	if !ok {
		http.NotFound(w, r)
		return
	}
	_, _ = w.Write(body)
}

func (s *releaseServer) count(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, p := range s.requests {
		if p == path {
			n++
		}
	}
	return n
}

func (s *releaseServer) source() source {
	return source{
		client:           &http.Client{},
		latestReleaseURL: s.srv.URL + "/latest-release.json",
		downloadURL:      s.srv.URL + "/download",
	}
}

func TestInstallDownloadsOnceThenReusesTheInstall(t *testing.T) {
	s, archivePath := release(t, "")
	root := filepath.Join(t.TempDir(), "python-build-standalone")
	var log bytes.Buffer
	opts := Options{PythonVersion: "3.14", Log: &log}

	dest, err := install(opts, root, testTriple, s.source())
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "3.14.1+freethreaded+pgo+lto"); dest != want {
		t.Errorf("dest %q, want %q", dest, want)
	}
	if got := readFile(t, filepath.Join(dest, "python", "PYTHON.json")); got != "hi" {
		t.Errorf("got %q", got)
	}
	// Only the install itself is left: no .download or .partial temp files.
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if !slices.Equal(names, []string{"3.14.1+freethreaded+pgo+lto"}) {
		t.Errorf("left in root: %q", names)
	}
	for _, line := range []string{"downloading cpython-3.14.1+", "extracting to ", "installed: " + dest} {
		if !strings.Contains(log.String(), line) {
			t.Errorf("log has no %q:\n%s", line, log.String())
		}
	}

	// A second run finds the install and does not fetch the archive again.
	log.Reset()
	if again, err := install(opts, root, testTriple, s.source()); err != nil || again != dest {
		t.Errorf("second run: %q, %v", again, err)
	}
	if !strings.Contains(log.String(), "already installed: "+dest) {
		t.Errorf("second run log:\n%s", log.String())
	}
	if n := s.count(archivePath); n != 1 {
		t.Errorf("archive fetched %d times", n)
	}
}

func TestInstallRejectsAChecksumMismatchAndLeavesNothing(t *testing.T) {
	s, _ := release(t, strings.Repeat("0", 64))
	root := t.TempDir()
	_, err := install(Options{Quiet: true}, root, testTriple, s.source())
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("got %v", err)
	}
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Errorf("left behind: %v", entries)
	}
}

func TestInstallReportsMissingBuildOptions(t *testing.T) {
	s, _ := release(t, "")
	root := filepath.Join(t.TempDir(), "python-build-standalone")
	_, err := install(Options{Variant: "pgo", Quiet: true}, root, testTriple, s.source())
	if err == nil || !strings.Contains(err.Error(), "available build options for that version: freethreaded+pgo+lto") {
		t.Fatalf("got %v", err)
	}
	if exists(root) {
		t.Error("created the root before choosing a build")
	}
}

func TestInstallRejectsABadVersionBeforeTheNetwork(t *testing.T) {
	s, _ := release(t, "")
	_, err := install(Options{PythonVersion: "20261111", Quiet: true}, t.TempDir(), testTriple, s.source())
	if err == nil || !strings.Contains(err.Error(), "invalid Python version") {
		t.Fatalf("got %v", err)
	}
	if n := s.count("/latest-release.json"); n != 0 {
		t.Errorf("looked up the release %d times", n)
	}
}

func TestInstallNotesFallbackBuild(t *testing.T) {
	s, _ := release(t, "")
	// Offer the archive only as pgo+lto.
	files := map[string][]byte{}
	for p, b := range s.files {
		files[strings.ReplaceAll(p, "freethreaded%2Bpgo%2Blto", "pgo%2Blto")] =
			bytes.ReplaceAll(b, []byte("freethreaded+pgo+lto"), []byte("pgo+lto"))
	}
	s.files = files
	var log bytes.Buffer
	dest, err := install(Options{Log: &log}, t.TempDir(), testTriple, s.source())
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(dest) != "3.14.1+pgo+lto" {
		t.Errorf("dest %q", dest)
	}
	if !strings.Contains(log.String(), "note: freethreaded+pgo+lto is not available for "+testTriple+"; using pgo+lto") {
		t.Errorf("log:\n%s", log.String())
	}
}

func TestOptionsDefaults(t *testing.T) {
	var o Options
	if o.pythonVersion() != DefaultPythonVersion {
		t.Errorf("default version %q", o.pythonVersion())
	}
	if o.log() != os.Stderr {
		t.Error("default log is not stderr")
	}
	var buf bytes.Buffer
	if (Options{Log: &buf}).log() != &buf {
		t.Error("Log ignored")
	}
	if (Options{Log: &buf, Quiet: true}).log() == &buf {
		t.Error("Quiet ignored")
	}
}
