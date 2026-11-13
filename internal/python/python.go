// Package python downloads a python-build-standalone distribution
// (https://github.com/astral-sh/python-build-standalone) into the user's data
// directory. It is the Go port of the cerebrai-python crate in
// cerebrai-app/cerebrai-crates, shared by brainctl and workctl.
//
// Layout: <data dir>/python-build-standalone/<version>+<build options>/ (e.g.
// 3.14.10+freethreaded+pgo+lto), holding the archive's own python/ tree. If
// that directory already exists nothing is downloaded. The data dir is
// $XDG_DATA_HOME if set to an absolute path, otherwise %LOCALAPPDATA% on
// Windows and ~/.local/share elsewhere.
//
// Each archive is checked against the release's SHA256SUMS. That catches
// corrupt or truncated downloads, but the sums come from the same GitHub
// release as the archive, so it does not protect against a compromised release.
package python

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	// DefaultPythonVersion is installed when Options.PythonVersion is empty.
	DefaultPythonVersion = "3.14"
	// DefaultVariant is the build preferred when Options.Variant is empty.
	DefaultVariant = "freethreaded+pgo+lto"
)

// Options says what Install should install. The zero value installs the
// newest DefaultPythonVersion with the default build options.
type Options struct {
	// PythonVersion is a Python version: 3 (newest 3.x), 3.14 (newest 3.14.x)
	// or 3.14.15 (exact). Not a python-build-standalone release date. Empty
	// means DefaultPythonVersion.
	PythonVersion string
	// Variant requires exact build options, e.g. pgo+lto. Empty means
	// DefaultVariant, falling back to the best build available.
	Variant string
	// Log receives progress, notes and the result line; nil means os.Stderr.
	// Nothing is ever written to stdout, so callers keep it for their own
	// output.
	Log io.Writer
	// Quiet discards everything that would go to Log.
	Quiet bool
}

func (o Options) pythonVersion() string {
	if o.PythonVersion == "" {
		return DefaultPythonVersion
	}
	return o.PythonVersion
}

func (o Options) log() io.Writer {
	switch {
	case o.Quiet:
		return io.Discard
	case o.Log != nil:
		return o.Log
	}
	return os.Stderr
}

// source is where releases are looked up and downloaded from. Tests point it
// at a local server.
type source struct {
	client           *http.Client
	latestReleaseURL string
	downloadURL      string
}

// Install installs the newest distribution matching opts and returns its
// directory, <data dir>/python-build-standalone/<version>+<build options>,
// which holds the archive's python/ tree.
//
// Nothing is downloaded if that directory already exists, but the latest
// release is looked up on every call, so this always needs network access. An
// invalid Options.PythonVersion is rejected before anything touches the
// network or the file system.
func Install(opts Options) (string, error) {
	home, err := dataHome(os.Getenv("XDG_DATA_HOME"), os.Getenv("HOME"),
		os.Getenv("LOCALAPPDATA"), os.Getenv("USERPROFILE"), runtime.GOOS == "windows")
	if err != nil {
		return "", err
	}
	triple, err := hostTriple(runtime.GOOS, runtime.GOARCH, systemLibc)
	if err != nil {
		return "", err
	}
	src := source{client: newClient(), latestReleaseURL: latestReleaseURL, downloadURL: releaseDownloadURL}
	return install(opts, filepath.Join(home, "python-build-standalone"), triple, src)
}

// install is Install with the install root, platform and release source given.
func install(opts Options, root, triple string, src source) (string, error) {
	want := opts.pythonVersion()
	log := opts.log()
	say := func(format string, args ...any) { _, _ = fmt.Fprintf(log, format+"\n", args...) }

	// Checked first, so a bad value fails before anything touches the network.
	if err := validatePythonVersion(want); err != nil {
		return "", err
	}
	tag, err := latestTag(src.client, src.latestReleaseURL)
	if err != nil {
		return "", err
	}
	sums, err := httpText(src.client, src.downloadURL+"/"+tag+"/SHA256SUMS")
	if err != nil {
		return "", err
	}
	assets := parseSums(sums, triple)

	variants := defaultVariants()
	if opts.Variant != "" {
		variants = []string{opts.Variant}
	}
	a, ok := selectAsset(assets, want, variants)
	if !ok {
		return "", fmt.Errorf("no python-build-standalone %s distribution for Python %s (%s) on %s.\n"+
			"available build options for that version: %s",
			tag, want, strings.Join(variants, ", "), triple, available(assets, want))
	}
	if opts.Variant == "" && a.buildOptions != DefaultVariant {
		say("note: %s is not available for %s; using %s", DefaultVariant, triple, a.buildOptions)
	}
	if note := newerVersionNote(assets, want, a); note != "" {
		say("%s", note)
	}

	dest := filepath.Join(root, a.dirName())
	if exists(dest) {
		say("already installed: %s", dest)
		return dest, nil
	}

	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	sweepStaleTemps(root, staleTempAge)
	// Temp names include the process id and the start time, so concurrent runs
	// of the same build don't delete each other's files even when two
	// containers sharing one data directory happen to have the same pid.
	unique := uniqueSuffix()
	archive := filepath.Join(root, a.dirName()+"."+unique+".download")
	partial := filepath.Join(root, a.dirName()+"."+unique+".partial")
	installed, err := downloadAndUnpack(opts, log, src, tag, a, archive, partial, dest)
	// Best-effort cleanup of the temporaries, whether or not that worked.
	_ = os.Remove(archive)
	_ = os.RemoveAll(partial)
	if err != nil {
		return "", err
	}
	if installed {
		say("installed: %s", dest)
	} else {
		say("already installed by another process: %s", dest)
	}
	return dest, nil
}

// downloadAndUnpack downloads, verifies and unpacks a into dest. It returns
// false if another process finished the same install first.
func downloadAndUnpack(opts Options, log io.Writer, src source, tag string, a asset, archive, partial, dest string) (bool, error) {
	_ = os.RemoveAll(partial)
	// '+' must be percent-encoded in the URL path.
	url := src.downloadURL + "/" + tag + "/" + strings.ReplaceAll(a.fileName, "+", "%2B")
	_, _ = fmt.Fprintf(log, "downloading %s\n", a.fileName)
	var progress io.Writer
	if !opts.Quiet {
		progress = log
	}
	if err := download(src.client, url, archive, a.sha256, progress); err != nil {
		return false, err
	}
	_, _ = fmt.Fprintf(log, "extracting to %s\n", dest)
	return unpackAndPublish(archive, partial, dest)
}
