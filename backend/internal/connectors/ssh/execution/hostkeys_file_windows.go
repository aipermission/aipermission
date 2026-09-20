//go:build windows

package execution

import (
	"fmt"

	"golang.org/x/sys/windows"
)

type windowsKnownHostsOps struct {
	move func(*uint16, *uint16, uint32) error
}

func nativeWindowsKnownHostsOps() windowsKnownHostsOps {
	return windowsKnownHostsOps{move: windows.MoveFileEx}
}

func renameKnownHostsFile(sourcePath, targetPath string) error {
	return renameKnownHostsFileWindows(sourcePath, targetPath, nativeWindowsKnownHostsOps())
}

func renameKnownHostsFileWindows(sourcePath, targetPath string, ops windowsKnownHostsOps) error {
	source, err := windows.UTF16PtrFromString(sourcePath)
	if err != nil {
		return fmt.Errorf("encode known_hosts replacement source: %w", err)
	}
	target, err := windows.UTF16PtrFromString(targetPath)
	if err != nil {
		return fmt.Errorf("encode known_hosts replacement target: %w", err)
	}
	if err := ops.move(source, target, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH); err != nil {
		return fmt.Errorf("atomically replace known_hosts: %w", err)
	}
	return nil
}

func syncKnownHostsDirectory(string) error {
	// MOVEFILE_WRITE_THROUGH flushes the replacement before returning. Windows
	// does not expose a portable directory handle that can be flushed here.
	return nil
}
