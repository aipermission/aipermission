//go:build linux

package maintenanceconsole

import (
	"errors"
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

func signalMaintenanceConsoleProcess(pid int, signal syscall.Signal) error {
	if pid < 1 {
		return nil
	}
	return syscall.Kill(pid, signal)
}

func terminateMaintenanceConsoleSupervisor(pid int) {
	if pid < 1 {
		return
	}
	_ = signalMaintenanceConsoleProcess(pid, syscall.SIGHUP)
	if waitForMaintenanceConsoleSupervisorExit(pid, maintenanceConsoleProcessGracePeriod) {
		return
	}
	_ = signalMaintenanceConsoleProcess(pid, syscall.SIGKILL)
}

func maintenanceConsoleProcessExists(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
