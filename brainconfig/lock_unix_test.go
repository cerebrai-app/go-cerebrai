//go:build unix

package brainconfig

import (
	"strings"
	"syscall"
	"testing"
	"time"
)

// A named pipe where the lock file goes is refused at once: opening one waits
// for a writer, so it would hang every change with no message.
func TestLockFileIsNamedPipe(t *testing.T) {
	path := writeConfig(t, brainOnly)
	if err := syscall.Mkfifo(lockPath(path), 0o600); err != nil {
		t.Skipf("cannot make a named pipe here: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := Set(path, TopLevel, "a", "b")
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "not a regular file") {
			t.Errorf("Set: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Set is still waiting on the named pipe after 5s")
	}
	if got := readFile(t, path); got != brainOnly {
		t.Errorf("file changed to %q", got)
	}
}
