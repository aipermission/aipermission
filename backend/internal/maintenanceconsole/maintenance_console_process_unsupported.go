//go:build !linux

package maintenanceconsole

import (
	"errors"
	"os/exec"
)

func configureMaintenanceConsoleProcess(_ *exec.Cmd) error {
	return errors.New("maintenance console is supported only on Linux")
}

func Supported() bool {
	return false
}
