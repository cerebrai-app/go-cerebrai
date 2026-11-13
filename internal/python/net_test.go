package python

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// sha256("hello")
const helloSHA256 = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"

func TestCopyVerifiedAcceptsMatchingHash(t *testing.T) {
	var out, progress bytes.Buffer
	if err := copyVerified(strings.NewReader("hello"), &out, 5, helloSHA256, &progress); err != nil {
		t.Fatal(err)
	}
	if out.String() != "hello" {
		t.Errorf("copied %q", out.String())
	}
	if progress.String() != "  100%\n" {
		t.Errorf("progress %q", progress.String())
	}
	// Hex case does not matter.
	if err := copyVerified(strings.NewReader("hello"), io.Discard, -1, strings.ToUpper(helloSHA256), nil); err != nil {
		t.Error(err)
	}
}

func TestCopyVerifiedRejectsWrongHashAndTamperedData(t *testing.T) {
	for _, tt := range []struct{ data, sum string }{
		{"hello", strings.Repeat("0", 64)},
		{"hellO", helloSHA256},
		{"", helloSHA256}, // empty input must not slip through either
	} {
		err := copyVerified(strings.NewReader(tt.data), io.Discard, -1, tt.sum, nil)
		if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
			t.Errorf("%q: got %v", tt.data, err)
		}
	}
}

func TestCopyVerifiedReportsWriteErrors(t *testing.T) {
	err := copyVerified(strings.NewReader("hello"), failingWriter{}, -1, helloSHA256, nil)
	if !errors.Is(err, errWrite) {
		t.Errorf("got %v", err)
	}
}

var errWrite = errors.New("disk full")

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errWrite }

func TestTagFromLatestReleaseIsValidated(t *testing.T) {
	if tag, err := tagFromLatestRelease(`{"version":1,"tag":"20261026"}`); err != nil || tag != "20261026" {
		t.Errorf("got %q, %v", tag, err)
	}
	for _, bad := range []string{
		`{"tag":"../x"}`, `{"tag":"a/b"}`, `{"tag":""}`, `{"tag":42}`, `{"version":1}`, "not json", "[]",
	} {
		if tag, err := tagFromLatestRelease(bad); err == nil {
			t.Errorf("%s: got %q", bad, tag)
		}
	}
}

func TestGetFailsOnHTTPErrors(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	_, err := httpText(&http.Client{}, srv.URL+"/missing")
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("got %v", err)
	}
}
