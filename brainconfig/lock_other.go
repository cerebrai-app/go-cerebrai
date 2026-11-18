//go:build !(darwin || dragonfly || freebsd || linux || netbsd || openbsd || windows)

package brainconfig

import "os"

// tryLock does not lock on this platform, which has no file lock this
// package uses: changes are not protected from each other here.
func tryLock(*os.File) (locked bool, err error) { return true, nil }

// unlockFile does nothing, as tryLock took no lock.
func unlockFile(*os.File) error { return nil }
