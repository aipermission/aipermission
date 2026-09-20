//go:build linux

package databasecatalog

import (
	"errors"
	"fmt"

	"github.com/aipermission/aipermission/backend/internal/db"
	"golang.org/x/sys/unix"
)

func renameFileNoReplace(sourcePath string, targetPath string) error {
	err := unix.Renameat2(unix.AT_FDCWD, sourcePath, unix.AT_FDCWD, targetPath, unix.RENAME_NOREPLACE)
	if errors.Is(err, unix.EEXIST) {
		return db.ErrPublishTargetExists
	}
	if err != nil {
		return fmt.Errorf("rename file without replacement: %w", err)
	}
	return nil
}
