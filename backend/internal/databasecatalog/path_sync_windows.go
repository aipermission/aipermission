//go:build windows

package databasecatalog

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

type windowsPathSyncOps struct {
	stat   func(string) (os.FileInfo, error)
	create func(*uint16, uint32, uint32, *windows.SecurityAttributes, uint32, uint32, windows.Handle) (windows.Handle, error)
	flush  func(windows.Handle) error
	close  func(windows.Handle) error
}

func syncDatabaseDeletePath(path string) error {
	return syncDatabaseDeletePathWindows(path, windowsPathSyncOps{
		stat: os.Stat, create: windows.CreateFile, flush: windows.FlushFileBuffers, close: windows.CloseHandle,
	})
}

func syncDatabaseDeletePathWindows(path string, ops windowsPathSyncOps) error {
	encoded, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return fmt.Errorf("encode database durability path: %w", err)
	}
	info, err := ops.stat(path)
	if err != nil {
		return err
	}
	flags := uint32(windows.FILE_ATTRIBUTE_NORMAL)
	if info.IsDir() {
		flags = windows.FILE_FLAG_BACKUP_SEMANTICS
	}
	handle, err := ops.create(
		encoded,
		windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		flags,
		0,
	)
	if err != nil {
		return fmt.Errorf("open database durability path: %w", err)
	}
	if err := ops.flush(handle); err != nil {
		_ = ops.close(handle)
		return fmt.Errorf("flush database durability path: %w", err)
	}
	if err := ops.close(handle); err != nil {
		return fmt.Errorf("close database durability path: %w", err)
	}
	return nil
}
