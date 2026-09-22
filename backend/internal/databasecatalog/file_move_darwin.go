//go:build darwin

package databasecatalog

import (
	"errors"
	"fmt"

	"github.com/aipermission/aipermission/backend/internal/db"
	"golang.org/x/sys/unix"
)

type darwinMoveOps struct {
	renameExclusive func(int, string, int, string, uint32) error
}

func nativeDarwinMoveOps() darwinMoveOps {
	return darwinMoveOps{renameExclusive: unix.RenameatxNp}
}

func renameFileNoReplace(sourcePath string, targetPath string) error {
	return renameFileNoReplaceDarwin(sourcePath, targetPath, nativeDarwinMoveOps())
}

func renameFileNoReplaceDarwin(sourcePath string, targetPath string, ops darwinMoveOps) error {
	err := ops.renameExclusive(unix.AT_FDCWD, sourcePath, unix.AT_FDCWD, targetPath, unix.RENAME_EXCL)
	if errors.Is(err, unix.EEXIST) {
		return db.ErrPublishTargetExists
	}
	if err != nil {
		return fmt.Errorf("rename file without replacement: %w", err)
	}
	return nil
}
