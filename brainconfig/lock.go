package brainconfig

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Every change to the config file holds an exclusive lock from before it
// reads the file until after it has replaced it, so two programs changing
// the file at once, say two extensions' init in one setup script, take turns
// instead of one silently losing the other's change.
//
// The lock is on a separate file beside the config, .config.yaml.lock: not
// on the config itself, which every edit replaces, so a lock on it would be
// left on the old file. The operating system's file lock (flock, or
// LockFileEx on Windows) goes away with the process that held it, so a crash
// never leaves the config locked.

// lockTimeout is how long a change waits for another one to finish before it
// gives up; tests shorten it.
var lockTimeout = 10 * time.Second

// lockPath is the lock file for the config file at path: beside the file
// path names, through a symlink, so every program that writes that file uses
// the same lock.
func lockPath(path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	return filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".lock")
}

// lockConfig takes the lock for the config file at path, waiting up to
// lockTimeout for whoever holds it, and returns the function that releases it.
// The lock file is made if missing, with the config file's permissions plus
// read and write for its owner, or owner-only if there is no config file yet.
// It is opened read-only, which is all the lock needs, so a read-only lock
// file, such as one an older release made beside a read-only config, still
// works.
func lockConfig(path string) (unlock func(), err error) {
	mode := fs.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		mode |= info.Mode().Perm()
	}
	lp := lockPath(path)
	f, err := os.OpenFile(lp, os.O_RDONLY|os.O_CREATE, mode)
	if err != nil {
		return nil, fmt.Errorf("locking %s: %w", path, err)
	}
	deadline := time.Now().Add(lockTimeout)
	for {
		locked, err := tryLock(f)
		if err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("locking %s: %w", path, err)
		}
		if locked {
			return func() {
				_ = unlockFile(f)
				_ = f.Close()
			}, nil
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, fmt.Errorf("%s: another program has been changing it for over %v (lock file %s): try again once it has finished",
				path, lockTimeout, lp)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// lockExisting is lockConfig for a change to a config file that must exist
// already: a missing one is an ErrNoConfig error, and no lock file is made
// for it.
func lockExisting(path string) (unlock func(), err error) {
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%s: %w", path, ErrNoConfig)
	}
	return lockConfig(path)
}
