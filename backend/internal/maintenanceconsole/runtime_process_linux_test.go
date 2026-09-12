//go:build linux

package maintenanceconsole

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
)

func maintenanceConsoleProcessIsRunning(pid int) bool {
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err == nil {
		closingParen := strings.LastIndexByte(string(stat), ')')
		if closingParen >= 0 {
			fields := strings.Fields(string(stat[closingParen+1:]))
			return len(fields) > 0 && fields[0] != "Z"
		}
	}
	err = syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
