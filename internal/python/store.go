package python

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

const (
	// renameAttempts and renameDelay: renaming the unpacked tree into place is
	// retried this many times, this long apart, while the destination still
	// does not exist.
	renameAttempts = 5
	renameDelay    = 200 * time.Millisecond

	// staleTempAge: leftover temp files older than this are removed at the
	// start of a run.
	staleTempAge = 24 * time.Hour
)

// renameRetrying is os.Rename, retried while to still does not exist. On
// Windows a virus scanner often holds files in a freshly unpacked tree, which
// makes the rename fail with "Access is denied" for a moment. If to has
// appeared (another process won the race) the error is returned at once for
// the caller to handle.
func renameRetrying(from, to string, attempts int, delay time.Duration) error {
	for attempt := 1; ; attempt++ {
		err := os.Rename(from, to)
		if err == nil || exists(to) || attempt >= attempts {
			return err
		}
		time.Sleep(delay)
	}
}

func exists(name string) bool {
	_, err := os.Lstat(name)
	return err == nil
}

// sweepStaleTemps removes *.download and *.partial entries in root that have
// not been modified for maxAge: leftovers from a run that was killed (e.g.
// Ctrl-C). Recent ones may belong to a run in progress, so they are left
// alone. Best effort.
func sweepStaleTemps(root string, maxAge time.Duration) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	now := time.Now()
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".download") && !strings.HasSuffix(name, ".partial") {
			continue
		}
		info, err := e.Info()
		if err != nil || now.Sub(info.ModTime()) < maxAge {
			continue
		}
		_ = os.RemoveAll(filepath.Join(root, name))
	}
}

// unpackAndPublish unpacks the .tar.zst archive into partial, then renames it
// to dest. It returns false if dest appeared in the meantime (another process
// won the race), leaving partial for the caller to clean up.
func unpackAndPublish(archive, partial, dest string) (bool, error) {
	if err := unpack(archive, partial); err != nil {
		return false, fmt.Errorf("extracting archive: %w", err)
	}
	if err := renameRetrying(partial, dest, renameAttempts, renameDelay); err != nil {
		// Another process may have finished the same install first.
		if exists(dest) {
			return false, nil
		}
		return false, fmt.Errorf("moving into %s: %w", dest, err)
	}
	return true, nil
}

func unpack(archive, dir string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	zr, err := zstd.NewReader(f)
	if err != nil {
		return err
	}
	defer zr.Close()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// Every file operation goes through root, which refuses any path that
	// leaves dir, whether by "..", an absolute name or a symlink. Closed
	// before returning, so that Windows lets the caller rename dir.
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	err = extract(tar.NewReader(zr), root)
	if cerr := root.Close(); err == nil {
		err = cerr
	}
	return err
}

// extract writes the entries of tr into root, keeping their permissions and
// modification times. Entries other than files, directories and links are
// skipped.
func extract(tr *tar.Reader, root *os.Root) error {
	type dir struct {
		name  string
		mode  fs.FileMode
		mtime time.Time
	}
	var dirs []dir
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		name := filepath.FromSlash(path.Clean(hdr.Name))
		mode := fs.FileMode(hdr.Mode).Perm()
		if hdr.Typeflag != tar.TypeDir {
			if parent := path.Dir(path.Clean(hdr.Name)); parent != "." {
				if err := root.MkdirAll(filepath.FromSlash(parent), 0o755); err != nil {
					return err
				}
			}
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := root.MkdirAll(name, 0o755); err != nil {
				return err
			}
			// Permissions are set at the end, so a read-only directory can
			// still be filled first.
			dirs = append(dirs, dir{name, mode, hdr.ModTime})
		case tar.TypeReg:
			if err := writeFile(root, name, mode, tr); err != nil {
				return err
			}
			if err := root.Chtimes(name, hdr.ModTime, hdr.ModTime); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := root.Symlink(hdr.Linkname, name); err != nil {
				return err
			}
		case tar.TypeLink:
			if err := root.Link(filepath.FromSlash(path.Clean(hdr.Linkname)), name); err != nil {
				return err
			}
		}
	}
	// Innermost first, so a parent's time is not changed by its children.
	for _, d := range slices.Backward(dirs) {
		if err := root.Chmod(d.name, d.mode); err != nil {
			return err
		}
		if err := root.Chtimes(d.name, d.mtime, d.mtime); err != nil {
			return err
		}
	}
	return nil
}

func writeFile(root *os.Root, name string, mode fs.FileMode, r io.Reader) error {
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, r)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	// The umask may have dropped bits from mode when the file was created.
	return root.Chmod(name, mode)
}

// uniqueSuffix is "<pid>-<nanoseconds since the epoch>".
func uniqueSuffix() string {
	return fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
}
