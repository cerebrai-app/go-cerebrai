package brainconfig

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// Many programs changing the file at once take turns: no change is lost.
func TestConcurrentChanges(t *testing.T) {
	path := writeConfig(t, brainOnly)
	const n = 16
	var wg sync.WaitGroup
	errs := make(chan error, 3*n)
	for i := range n {
		wg.Add(3)
		go func() {
			defer wg.Done()
			_, err := Set(path, TopLevel, fmt.Sprintf("key%02d", i), "v")
			errs <- err
		}()
		go func() {
			defer wg.Done()
			_, err := AddExtension(path, fmt.Sprintf("com.example.tool%02d", i), ExtensionBase{SchemaVersion: 1, Bin: "tool"})
			errs <- err
		}()
		go func() {
			defer wg.Done()
			_, err := Init(path)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := range n {
		if got, err := cfg.Get(TopLevel, fmt.Sprintf("key%02d", i)); err != nil || got != "v" {
			t.Errorf("key%02d: %q, %v", i, got, err)
		}
		if _, ok, err := cfg.Extension(fmt.Sprintf("com.example.tool%02d", i)); err != nil || !ok {
			t.Errorf("com.example.tool%02d: %v, %v", i, ok, err)
		}
	}
}

// A change waits for the one before it, and gives up with a clear error, and
// without touching the file, if that one does not finish.
func TestLockTimeout(t *testing.T) {
	old := lockTimeout
	t.Cleanup(func() { lockTimeout = old })
	lockTimeout = 100 * time.Millisecond

	path := writeConfig(t, brainOnly)
	unlock, err := lockConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err = Set(path, TopLevel, "owner", "me")
	if err == nil || !strings.Contains(err.Error(), "another program has been changing it") {
		t.Errorf("Set while locked: %v", err)
	}
	if waited := time.Since(start); waited < lockTimeout {
		t.Errorf("gave up after %v, before the timeout", waited)
	}
	if got := readFile(t, path); got != brainOnly {
		t.Errorf("file changed to %q", got)
	}
	for name, change := range map[string]func() error{
		"Init":         func() error { _, err := Init(path); return err },
		"AddExtension": func() error { _, err := AddExtension(path, workctlID, workctlBase); return err },
		"Unset":        func() error { _, err := Unset(path, TopLevel, "owner"); return err },
	} {
		if err := change(); err == nil || !strings.Contains(err.Error(), "another program") {
			t.Errorf("%s while locked: %v", name, err)
		}
	}

	// Once released, the change goes through.
	unlock()
	if changed, err := Set(path, TopLevel, "owner", "me"); err != nil || !changed {
		t.Errorf("Set after unlock: %v, %v", changed, err)
	}
}

// The lock is held across the whole change: a change that waited sees what
// the one before it wrote.
func TestLockWaitsThenProceeds(t *testing.T) {
	path := writeConfig(t, brainOnly)
	unlock, err := lockConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error)
	go func() {
		_, err := Set(path, TopLevel, "b", "2")
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("Set did not wait for the lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	// Change the file while holding the lock, as another program would.
	if err := writeFile(path, []byte(brainOnly+"a: \"1\"\n")); err != nil {
		t.Fatal(err)
	}
	unlock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got, want := readFile(t, path), brainOnly+"a: \"1\"\nb: \"2\"\n"; got != want {
		t.Errorf("file = %q, want %q", got, want)
	}
}

// Every program locks the file a symlink points to, not the link.
func TestLockPathFollowsSymlinks(t *testing.T) {
	real := filepath.Join(t.TempDir(), "brain.yaml")
	if err := os.WriteFile(real, []byte(brainOnly), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("cannot make a symlink here: %v", err)
	}
	if got, want := lockPath(link), filepath.Join(filepath.Dir(real), ".brain.yaml.lock"); got != want {
		// On macOS the temporary directory is itself behind a symlink.
		if r, err := filepath.EvalSymlinks(filepath.Dir(want)); err != nil || got != filepath.Join(r, ".brain.yaml.lock") {
			t.Errorf("lockPath = %q, want %q", got, want)
		}
	}
	if got, want := lockPath(Path("/p")), filepath.Join("/p", ".brain", ".config.yaml.lock"); got != want {
		t.Errorf("lockPath of a missing file = %q, want %q", got, want)
	}
}

// Changes to a missing file make no lock file, so a mistaken BRAIN_HOME is
// not littered with one.
func TestNoLockFileWithoutConfig(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".brain")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, FileName)
	if _, err := Set(path, TopLevel, "a", "b"); !errors.Is(err, ErrNoConfig) {
		t.Errorf("Set: %v", err)
	}
	if _, err := AddExtension(path, workctlID, workctlBase); !errors.Is(err, ErrNoConfig) {
		t.Errorf("AddExtension: %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("made %v", entries)
	}
}

func TestLockFileCannotBeMade(t *testing.T) {
	path := writeConfig(t, brainOnly)
	// A directory where the lock file goes.
	if err := os.Mkdir(lockPath(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Set(path, TopLevel, "a", "b"); err == nil || !strings.Contains(err.Error(), "locking "+path) {
		t.Errorf("Set: %v", err)
	}
	if got := readFile(t, path); got != brainOnly {
		t.Errorf("file changed to %q", got)
	}
}

// Init makes the file and its directory owner-only; edits keep whatever the
// user set since, and the lock file takes the config file's permissions.
func TestPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no Unix permissions")
	}
	path := Path(t.TempDir())
	if _, err := Init(path); err != nil {
		t.Fatal(err)
	}
	for p, want := range map[string]fs.FileMode{path: 0o600, filepath.Dir(path): 0o700, lockPath(path): 0o600} {
		if info, err := os.Stat(p); err != nil || info.Mode().Perm() != want {
			t.Errorf("%s: %v, %v; want %v", p, info.Mode().Perm(), err, want)
		}
	}

	// Shared on purpose: edits keep it shared.
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := Set(path, TopLevel, "owner", "me"); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o640 {
		t.Errorf("after an edit: %v, %v", info.Mode().Perm(), err)
	}
}

// Init's new file appears whole, and leaves no temporary file behind, nor a
// file at all if it could not be written.
func TestInitCreatesWholeFile(t *testing.T) {
	path := Path(t.TempDir())
	if created, err := Init(path); err != nil || !created {
		t.Fatalf("Init: %v, %v", created, err)
	}
	if got := readFile(t, path); got != brainOnly {
		t.Errorf("file = %q", got)
	}
	if names := dirEntries(t, filepath.Dir(path)); len(names) != 1 || names[0] != FileName {
		t.Errorf("left %q", names)
	}
}

// createFile never replaces a file that appeared meanwhile, as one made by a
// program that does not take the lock.
func TestCreateFileKeepsExisting(t *testing.T) {
	path := writeConfig(t, "# someone else's\nschema_version: 1\n")
	if err := createFile(path, []byte(brainOnly)); !errors.Is(err, fs.ErrExist) {
		t.Errorf("createFile: %v", err)
	}
	if got := readFile(t, path); got != "# someone else's\nschema_version: 1\n" {
		t.Errorf("file changed to %q", got)
	}
	if names := dirEntries(t, filepath.Dir(path)); len(names) != 1 {
		t.Errorf("left %q", names)
	}
}
