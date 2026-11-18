//go:build !(darwin || dragonfly || freebsd || linux || netbsd || openbsd || windows)

package brainconfig

import (
	"errors"
	"os"
)

// errNoLocking is the error for changing the config file on a platform this
// package has no file lock for: without one, changes from two programs at
// once could silently lose one, so they fail instead.
var errNoLocking = errors.New("changing the config file is not supported on this platform: it has no file lock")

// tryLock fails: there is no file lock on this platform.
func tryLock(*os.File) (locked bool, err error) { return false, errNoLocking }

// unlockFile does nothing, as tryLock took no lock.
func unlockFile(*os.File) error { return nil }
