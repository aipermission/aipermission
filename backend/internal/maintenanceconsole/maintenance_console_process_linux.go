//go:build linux

package maintenanceconsole

import (
	"os/exec"
	"syscall"
)

func configureMaintenanceConsoleProcess(command *exec.Cmd) error {
	command.SysProcAttr = &syscall.SysProcAttr{Setctty: true, Setsid: true, Pdeathsig: syscall.SIGTERM}
	return nil
}

func Supported() bool {
	return true
}
