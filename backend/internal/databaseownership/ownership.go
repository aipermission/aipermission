package databaseownership

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var ErrDatabaseInUse = errors.New("database is in use by another AIPermission process")

type Ownership struct {
	mu   sync.Mutex
	file *os.File
}

func Acquire(databasePath string) (*Ownership, error) {
	absolutePath, err := filepath.Abs(filepath.Clean(databasePath))
	if err != nil {
		return nil, fmt.Errorf("resolve database ownership path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absolutePath), 0o700); err != nil {
		return nil, fmt.Errorf("create database ownership directory: %w", err)
	}
	file, err := openDatabaseOwnershipFile(absolutePath + ".owner.lock")
	if err != nil {
		return nil, err
	}
	if err := lockDatabaseOwnershipFile(file); err != nil {
		_ = file.Close()
		if isDatabaseOwnershipConflict(err) {
			return nil, ErrDatabaseInUse
		}
		return nil, fmt.Errorf("lock database ownership: %w", err)
	}
	return &Ownership{file: file}, nil
}

func (ownership *Ownership) Close() error {
	_, err := ownership.Release()
	return err
}

// Release reports whether the OS lock was conclusively released. A failed
// unlock retains the handle so a later shutdown attempt can retry it.
func (ownership *Ownership) Release() (bool, error) {
	if ownership == nil {
		return true, nil
	}
	ownership.mu.Lock()
	defer ownership.mu.Unlock()
	if ownership.file == nil {
		return true, nil
	}
	file := ownership.file
	if err := unlockDatabaseOwnershipFile(file); err != nil {
		return false, err
	}
	closeErr := file.Close()
	ownership.file = nil
	return true, closeErr
}
