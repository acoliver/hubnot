//go:build unix

package main

import (
	"errors"
	"os"
	"syscall"
)

// tryServeFileLock makes one non-blocking attempt at an exclusive or shared
// flock(2).
func tryServeFileLock(file *os.File, kind serveLockKind) (bool, error) {
	operation := syscall.LOCK_EX
	if kind == serveLockShared {
		operation = syscall.LOCK_SH
	}
	raw, err := file.SyscallConn()
	if err != nil {
		return false, err
	}
	for {
		var lockErr error
		if err := raw.Control(func(descriptor uintptr) {
			lockErr = syscall.Flock(int(descriptor), operation|syscall.LOCK_NB)
		}); err != nil {
			return false, err
		}
		switch {
		case lockErr == nil:
			return true, nil
		case errors.Is(lockErr, syscall.EINTR):
			continue
		case errors.Is(lockErr, syscall.EWOULDBLOCK):
			return false, nil
		default:
			return false, lockErr
		}
	}
}
