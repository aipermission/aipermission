//go:build windows

package databasecatalog

import (
	"errors"
	"fmt"

	"github.com/aipermission/aipermission/backend/internal/db"
	"golang.org/x/sys/windows"
)

type windowsMoveOps struct {
	create func(*uint16, uint32, uint32, *windows.SecurityAttributes, uint32, uint32, windows.Handle) (windows.Handle, error)
	flush  func(windows.Handle) error
	close  func(windows.Handle) error
	move   func(*uint16, *uint16, uint32) error
}

func nativeWindowsMoveOps() windowsMoveOps {
	return windowsMoveOps{
		create: windows.CreateFile,
		flush:  windows.FlushFileBuffers,
		close:  windows.CloseHandle,
		move:   windows.MoveFileEx,
	}
}

func syncFileForMove(path string) error {
	return syncFileForMoveWindows(path, nativeWindowsMoveOps())
}

func syncFileForMoveWindows(path string, ops windowsMoveOps) error {
	encoded, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return fmt.Errorf("encode move source path: %w", err)
	}
	handle, err := ops.create(
		encoded,
		windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return fmt.Errorf("open move source for flush: %w", err)
	}
	if err := ops.flush(handle); err != nil {
		_ = ops.close(handle)
		return fmt.Errorf("flush move source: %w", err)
	}
	if err := ops.close(handle); err != nil {
		return fmt.Errorf("close move source: %w", err)
	}
	return nil
}

func renameFileNoReplace(sourcePath string, targetPath string) error {
	return renameFileNoReplaceWindows(sourcePath, targetPath, nativeWindowsMoveOps())
}

func renameFileNoReplaceWindows(sourcePath string, targetPath string, ops windowsMoveOps) error {
	source, err := windows.UTF16PtrFromString(sourcePath)
	if err != nil {
		return fmt.Errorf("encode move source path: %w", err)
	}
	target, err := windows.UTF16PtrFromString(targetPath)
	if err != nil {
		return fmt.Errorf("encode move target path: %w", err)
	}
	err = ops.move(source, target, windows.MOVEFILE_WRITE_THROUGH)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) || errors.Is(err, windows.ERROR_FILE_EXISTS) {
		return db.ErrPublishTargetExists
	}
	if err != nil {
		return fmt.Errorf("rename file without replacement: %w", err)
	}
	return nil
}

func syncMoveDirectories(_, _ string) error {
	// MOVEFILE_WRITE_THROUGH does not return until the move has been flushed.
	// Windows directories cannot be portably flushed through an os.File handle.
	return nil
}
