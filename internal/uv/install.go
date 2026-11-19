package uv

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// maxBinarySize caps each file taken from the archive; uv is about 40 MB.
const maxBinarySize = 512 << 20

// source is where uv releases are downloaded from. Tests point it at a local
// server.
type source struct {
	client      *http.Client
	downloadURL string
}

// archiveName is the name of uv's release archive for triple. gen/main.go
// has a copy.
func archiveName(triple string) string {
	if strings.HasSuffix(triple, "-windows-msvc") {
		return "uv-" + triple + ".zip"
	}
	return "uv-" + triple + ".tar.gz"
}

// binaries are the files Install takes from the archive for triple.
func binaries(triple string) []string {
	if strings.HasSuffix(triple, "-windows-msvc") {
		return []string{"uv.exe", "uvx.exe", "uvw.exe"}
	}
	return []string{"uv", "uvx"}
}

// Install installs uv Version into d.Bin, unless it is there already. It needs
// network access only when it downloads uv.
func Install(d Dirs, opts Options) error {
	triple, err := hostTriple(runtime.GOOS, runtime.GOARCH, systemLibc)
	if err != nil {
		return err
	}
	return install(d, triple, source{client: newClient(), downloadURL: releaseDownloadURL}, opts)
}

// installedVersion is the version d's uv reports, or "" if it doesn't run.
// Tests replace it.
var installedVersion = func(d Dirs) string {
	var out bytes.Buffer
	cmd, err := d.Command(io.Discard, d.UV(), "--version")
	if err != nil {
		return ""
	}
	cmd.Stdout = &out
	if cmd.Run() != nil {
		return ""
	}
	// "uv 0.13.9 (x86_64-unknown-linux-gnu)", or "uv 0.13.9 (<commit> <date>)".
	fields := strings.Fields(out.String())
	if len(fields) < 2 || fields[0] != "uv" {
		return ""
	}
	return fields[1]
}

// install is Install with the platform and release source given.
func install(d Dirs, triple string, src source, opts Options) error {
	log := opts.log()
	say := func(format string, args ...any) { _, _ = fmt.Fprintf(log, format+"\n", args...) }

	sum, ok := archiveSHA256[triple]
	if !ok {
		return fmt.Errorf("uv %s has no release for %s", Version, triple)
	}
	if v := installedVersion(d); v == Version {
		say("uv %s already installed: %s", Version, d.UV())
		return nil
	} else if v != "" {
		say("replacing uv %s with uv %s", v, Version)
	}

	if err := os.MkdirAll(d.Bin, 0o755); err != nil {
		return err
	}
	name := archiveName(triple)
	unique := strconv.Itoa(os.Getpid()) + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	archive := filepath.Join(d.Root, name+"."+unique+".download")
	defer func() { _ = os.Remove(archive) }()
	say("downloading uv %s (%s)", Version, name)
	var progress io.Writer
	if !opts.Quiet {
		progress = log
	}
	if err := download(src.client, src.downloadURL+"/"+Version+"/"+name, archive, sum, progress); err != nil {
		return err
	}
	files, err := extract(archive, strings.HasSuffix(name, ".zip"), binaries(triple))
	if err != nil {
		return fmt.Errorf("%s: %w", archive, err)
	}
	// Each file is written next to its final name, then renamed over it, so a
	// uv that is running keeps working and a failed install leaves no half
	// written file.
	for _, file := range binaries(triple) {
		dest := filepath.Join(d.Bin, file)
		tmp := dest + "." + unique + ".tmp"
		if err := os.WriteFile(tmp, files[file], 0o755); err != nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("%s: %w", tmp, err)
		}
		if err := os.Rename(tmp, dest); err != nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("%s: %w", dest, err)
		}
	}
	say("installed uv %s: %s", Version, d.UV())
	return nil
}

// extract returns the contents of the files in archive, a .zip if isZip and
// otherwise a .tar.gz, whose base names are in names; each must be there. Directories in the
// archive are ignored and nothing is written to disk, so its paths cannot
// escape anywhere.
func extract(archive string, isZip bool, names []string) (map[string][]byte, error) {
	files := map[string][]byte{}
	take := func(name string, size int64, open func() (io.ReadCloser, error)) error {
		base := path.Base(strings.ReplaceAll(name, `\`, "/"))
		if !containsFold(names, base) || files[base] != nil {
			return nil
		}
		if size > maxBinarySize {
			return fmt.Errorf("%s: larger than %d bytes", name, maxBinarySize)
		}
		r, err := open()
		if err != nil {
			return err
		}
		defer func() { _ = r.Close() }()
		data, err := io.ReadAll(io.LimitReader(r, maxBinarySize+1))
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if len(data) > maxBinarySize {
			return fmt.Errorf("%s: larger than %d bytes", name, maxBinarySize)
		}
		files[base] = data
		return nil
	}

	if isZip {
		z, err := zip.OpenReader(archive)
		if err != nil {
			return nil, err
		}
		defer func() { _ = z.Close() }()
		for _, f := range z.File {
			if f.FileInfo().Mode().IsRegular() {
				if err := take(f.Name, int64(f.UncompressedSize64), f.Open); err != nil {
					return nil, err
				}
			}
		}
	} else {
		f, err := os.Open(archive)
		if err != nil {
			return nil, err
		}
		defer func() { _ = f.Close() }()
		gz, err := gzip.NewReader(f)
		if err != nil {
			return nil, err
		}
		tr := tar.NewReader(gz)
		for {
			h, err := tr.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return nil, err
			}
			if h.Typeflag == tar.TypeReg {
				open := func() (io.ReadCloser, error) { return io.NopCloser(tr), nil }
				if err := take(h.Name, h.Size, open); err != nil {
					return nil, err
				}
			}
		}
	}
	for _, name := range names {
		if files[name] == nil {
			return nil, fmt.Errorf("no %s in the archive", name)
		}
	}
	return files, nil
}
