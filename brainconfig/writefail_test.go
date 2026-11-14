package brainconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Failures while writing the config file: none of them may leave a partial
// file, a temporary file, or a changed original behind.

// longName is a file name that is valid on its own, but too long once
// writeFile turns it into a temporary file name (".<name>.<random>.tmp"):
// file systems limit a name to 255 bytes.
var longName = strings.Repeat("c", 240)

// dirEntries are the names in dir.
func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
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
	if err := os.WriteFile(path, []byte(brainOnly), 0o644); err != nil {
		t.Skipf("file system does not take a %d byte name: %v", len(longName), err)
	}
	added, err := AddExtension(path, "app.cerebrai.workctl", 1)
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
