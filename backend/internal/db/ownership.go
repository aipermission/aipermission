package db

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var ErrDatabaseInUse = errors.New("database is in use by another AIPermission process")

type DatabaseOwnership struct {
	file *os.File
}

func AcquireDatabaseOwnership(databasePath string) (*DatabaseOwnership, error) {
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
	return &DatabaseOwnership{file: file}, nil
}

func (ownership *DatabaseOwnership) Close() error {
	if ownership == nil || ownership.file == nil {
		return nil
	}
	file := ownership.file
	ownership.file = nil
	unlockErr := unlockDatabaseOwnershipFile(file)
	closeErr := file.Close()
	return errors.Join(unlockErr, closeErr)
}
