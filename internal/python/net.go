package python

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	latestReleaseURL   = "https://raw.githubusercontent.com/astral-sh/python-build-standalone/latest-release/latest-release.json"
	releaseDownloadURL = "https://github.com/astral-sh/python-build-standalone/releases/download"

	connectTimeout = 30 * time.Second
	// readTimeout ends a request whose response stalls for this long. There is
	// no total timeout: the archive is large.
	readTimeout = 60 * time.Second
	// maxTextSize caps latest-release.json and SHA256SUMS, which are small.
	maxTextSize = 16 << 20
)

// newClient returns a client that uses the system trust store and the proxy
// from HTTPS_PROXY / HTTP_PROXY / NO_PROXY, like the default client, with
// connect and header timeouts. Stalls in a body are handled by get.
func newClient() *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DialContext = (&net.Dialer{Timeout: connectTimeout, KeepAlive: 30 * time.Second}).DialContext
	t.TLSHandshakeTimeout = connectTimeout
	t.ResponseHeaderTimeout = readTimeout
	return &http.Client{Transport: t}
}

var errStalled = fmt.Errorf("no data for %s", readTimeout)

// response is a successful GET whose body fails with errStalled if a read
// waits longer than readTimeout.
type response struct {
	body          io.ReadCloser
	contentLength int64
	ctx           context.Context
	timer         *time.Timer
	cancel        context.CancelCauseFunc
}

// get fetches url and fails on any status but 200 OK.
func get(client *http.Client, url string) (*response, error) {
	ctx, cancel := context.WithCancelCause(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		cancel(nil)
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		cancel(nil)
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		cancel(nil)
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	r := &response{body: resp.Body, contentLength: resp.ContentLength, ctx: ctx, cancel: cancel}
	r.timer = time.AfterFunc(readTimeout, func() { cancel(errStalled) })
	return r, nil
}

func (r *response) Read(p []byte) (int, error) {
	n, err := r.body.Read(p)
	r.timer.Reset(readTimeout)
	if err != nil && errors.Is(context.Cause(r.ctx), errStalled) {
		err = errStalled
	}
	return n, err
}

func (r *response) Close() error {
	r.timer.Stop()
	err := r.body.Close()
	r.cancel(nil)
	return err
}

// httpText returns the body of url as a string.
func httpText(client *http.Client, url string) (string, error) {
	resp, err := get(client, url)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp, maxTextSize+1))
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", url, err)
	}
	if len(body) > maxTextSize {
		return "", fmt.Errorf("reading %s: larger than %d bytes", url, maxTextSize)
	}
	return string(body), nil
}

// download saves url to out and checks its SHA-256 against expectedSHA256.
// Progress goes to progress, if not nil.
func download(client *http.Client, url, out, expectedSHA256 string, progress io.Writer) error {
	resp, err := get(client, url)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Close() }()
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	err = copyVerified(resp, f, resp.contentLength, expectedSHA256, progress)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("downloading %s: %w", url, err)
	}
	return nil
}

// copyVerified copies r to w, then checks the SHA-256 of what was copied
// against expected (hex, either case). This is the only integrity check on the
// downloaded archive. A positive total, the expected size, turns on progress
// lines every 10% to progress, if not nil.
func copyVerified(r io.Reader, w io.Writer, total int64, expected string, progress io.Writer) error {
	h := sha256.New()
	buf := make([]byte, 256<<10)
	var done, lastPct int64
	for {
		n, err := r.Read(buf)
		if n > 0 {
			h.Write(buf[:n])
			if _, werr := w.Write(buf[:n]); werr != nil {
				return werr
			}
			done += int64(n)
			if progress != nil && total > 0 {
				if pct := done * 100 / total / 10 * 10; pct > lastPct {
					_, _ = fmt.Fprintf(progress, "  %d%%\n", pct)
					lastPct = pct
				}
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading download: %w", err)
		}
	}
	actual := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expected, actual)
	}
	return nil
}

// latestTag looks up the newest python-build-standalone release.
func latestTag(client *http.Client, url string) (string, error) {
	body, err := httpText(client, url)
	if err != nil {
		return "", err
	}
	return tagFromLatestRelease(body)
}

// tagFromLatestRelease reads the release tag from latest-release.json. It
// ends up in a URL, so it is checked by validateTag.
func tagFromLatestRelease(body string) (string, error) {
	var doc map[string]any
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		return "", fmt.Errorf("parsing latest-release.json: %w", err)
	}
	tag, ok := doc["tag"].(string)
	if !ok {
		return "", errors.New(`latest-release.json has no "tag"`)
	}
	if err := validateTag(tag); err != nil {
		return "", err
	}
	return tag, nil
}
