//go:build !windows

package db

import (
	"errors"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func openDatabaseOwnershipFile(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

func lockDatabaseOwnershipFile(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
}

func unlockDatabaseOwnershipFile(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_UN)
}

func isDatabaseOwnershipConflict(err error) bool {
	return errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN)
}
