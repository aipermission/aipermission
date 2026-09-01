//go:build !linux

package api

import (
	"errors"
	"os/exec"
)

func configureMaintenanceConsoleProcess(_ *exec.Cmd) error {
	return errors.New("maintenance console is supported only on Linux")
}

func maintenanceConsoleSupported() bool {
	return false
}
