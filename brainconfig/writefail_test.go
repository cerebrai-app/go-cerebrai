package brainconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Failures while writing the config file: none of them may leave a partial
// file, a temporary file, or a changed original behind.

// longName is a file name that is valid on its own, and so is its lock file's
// (".<name>.lock", 6 more), but not a temporary file's: file systems limit a
// name to 255 bytes, and ".<name>.<random>.tmp" adds at least 7 (the random
// part is 1 to 10 digits), so 249 is over the limit whatever the random part
// is, while the lock file's name is exactly 255.
var longName = strings.Repeat("c", 249)

// dirEntries are the names in dir, but for lock files (".<name>.lock"), which
// are meant to stay: what a write left behind besides the files it wrote.
func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") && strings.HasSuffix(e.Name(), ".lock") {
			continue
		}
		names = append(names, e.Name())
	}
	return names
}

func TestInitCannotCreateFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, strings.Repeat("c", 300)) // longer than any name may be
	if created, err := Init(path); err == nil || created {
		t.Errorf("Init: %v, %v", created, err)
	}
	if names := dirEntries(t, dir); len(names) != 0 {
		t.Errorf("left %q behind", names)
	}
}

func TestAddExtensionCannotWriteTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, longName)
	writeLong(t, path)
	added, err := addSettings(path, "app.cerebrai.workctl", 1)
	if err == nil || added || !strings.Contains(err.Error(), "writing "+path) {
		t.Errorf("AddExtension: %v, %v", added, err)
	}
	if got := readFile(t, path); got != brainOnly {
		t.Errorf("file changed to %q", got)
	}
	if names := dirEntries(t, dir); len(names) != 1 {
		t.Errorf("left files behind: %q", names)
	}
}

func TestWriteFileMissingTarget(t *testing.T) {
	dir := t.TempDir()
	if err := writeFile(filepath.Join(dir, FileName), []byte(brainOnly)); !os.IsNotExist(err) {
		t.Errorf("writeFile: %v", err)
	}
	if names := dirEntries(t, dir); len(names) != 0 {
		t.Errorf("left %q behind", names)
	}
}

func TestWriteFileCannotReplaceTarget(t *testing.T) {
	dir := t.TempDir()
	// A non-empty directory where the file should be: the temporary file is
	// written, but cannot be renamed over it.
	target := filepath.Join(dir, FileName)
	if err := os.MkdirAll(filepath.Join(target, "inside"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(target, []byte(brainOnly)); err == nil {
		t.Error("writeFile replaced a directory")
	}
	if names := dirEntries(t, dir); len(names) != 1 || names[0] != FileName {
		t.Errorf("left files behind: %q", names)
	}
	if info, err := os.Stat(target); err != nil || !info.IsDir() {
		t.Errorf("target changed: %v, %v", info, err)
	}
}

func TestWriteFileReplacesAtomically(t *testing.T) {
	path := writeConfig(t, brainOnly)
	const updated = "schema_version: 1\nextensions: {}\n"
	if err := writeFile(path, []byte(updated)); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); got != updated {
		t.Errorf("file = %q, want %q", got, updated)
	}
	if names := dirEntries(t, filepath.Dir(path)); len(names) != 1 {
		t.Errorf("left files behind: %q", names)
	}
}

// writeLong writes a valid config file at path, whose name is longName,
// skipping the test if the file system cannot take a name that long.
func writeLong(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(brainOnly), 0o644); err != nil {
		t.Skipf("file system does not take a %d byte name: %v", len(longName), err)
	}
}

// On Windows, a rename over a file another program has open fails until it
// closes it; replace retries for a while there, and only there.
func TestReplaceRetries(t *testing.T) {
	oldRename, oldRetry := rename, retryRename
	t.Cleanup(func() { rename, retryRename = oldRename, oldRetry })

	failures := 0
	blocked := func(n int) func(string, string) error {
		failures = 0
		return func(from, to string) error {
			if failures < n {
				failures++
				return &os.LinkError{Op: "rename", Old: from, New: to, Err: os.ErrPermission}
			}
			return os.Rename(from, to)
		}
	}

	retryRename = true
	rename = blocked(3)
	path := writeConfig(t, brainOnly)
	if err := writeFile(path, []byte("schema_version: 1\nowner: me\n")); err != nil || failures != 3 {
		t.Errorf("blocked 3 times: %v after %d failures", err, failures)
	}
	if got := readFile(t, path); got != "schema_version: 1\nowner: me\n" {
		t.Errorf("file = %q", got)
	}

	// It gives up eventually, leaving the file and no temporary file.
	rename = blocked(1000)
	if err := writeFile(path, []byte(brainOnly)); err == nil || failures != 10 {
		t.Errorf("blocked for good: %v after %d tries", err, failures)
	}
	if got := readFile(t, path); got != "schema_version: 1\nowner: me\n" {
		t.Errorf("file changed to %q", got)
	}
	if names := dirEntries(t, filepath.Dir(path)); len(names) != 1 {
		t.Errorf("left %q", names)
	}

	// Elsewhere a failed rename is not retried.
	retryRename = false
	rename = blocked(1)
	if err := writeFile(path, []byte(brainOnly)); err == nil || failures != 1 {
		t.Errorf("without retries: %v after %d tries", err, failures)
	}
}
