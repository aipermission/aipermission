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

func terminateMaintenanceConsoleSupervisor(process *os.Process) {
	if process == nil {
		return
	}
	// Keep the exec-created process handle; looking the PID up again can target
	// an unrelated process after the concurrent waiter reaps this supervisor.
	_ = process.Signal(syscall.SIGHUP)
	if waitForMaintenanceConsoleSupervisorExit(process, maintenanceConsoleProcessGracePeriod) {
		return
	}
	_ = process.Kill()
}

func maintenanceSupervisorExists(process *os.Process) bool {
	err := process.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}

func maintenanceConsoleProcessExists(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
