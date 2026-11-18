//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package brainconfig

import (
	"errors"
	"os"
	"syscall"
)

// tryLock takes an exclusive flock on f without waiting. locked is false if
// another open file holds it.
func tryLock(f *os.File) (locked bool, err error) {
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return false, nil
	}
	return err == nil, err
}

// unlockFile releases the lock tryLock took on f.
func unlockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
