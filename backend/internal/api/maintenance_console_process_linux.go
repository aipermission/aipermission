//go:build linux

package api

import (
	"os/exec"
	"syscall"
)

func configureMaintenanceConsoleProcess(command *exec.Cmd) error {
	command.SysProcAttr = &syscall.SysProcAttr{Setctty: true, Setsid: true, Pdeathsig: syscall.SIGTERM}
	return nil
}

func maintenanceConsoleSupported() bool {
	return true
}
