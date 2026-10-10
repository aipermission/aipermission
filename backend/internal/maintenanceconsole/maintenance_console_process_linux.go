//go:build linux

package maintenanceconsole

import (
	"errors"
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

func configureMaintenanceConsoleProcess(command *exec.Cmd) error {
	if _, _, err := maintenanceSupervisorProcessTree(os.Getpid()); err != nil {
		return err
	}
	if err := maintenancePIDHandlesAvailable(); err != nil {
		return err
	}
	command.SysProcAttr = &syscall.SysProcAttr{Setctty: true, Setsid: true, Pdeathsig: syscall.SIGTERM}
	return nil
}

func Supported() bool {
	_, _, err := maintenanceSupervisorProcessTree(os.Getpid())
	return err == nil && maintenancePIDHandlesAvailable() == nil
}

func maintenancePIDHandlesAvailable() error {
	fd, err := unix.PidfdOpen(os.Getpid(), 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	return unix.PidfdSendSignal(fd, 0, nil, 0)
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
