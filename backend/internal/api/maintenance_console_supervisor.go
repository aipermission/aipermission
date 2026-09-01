package api

import (
	"fmt"
	"os"
	"time"
)

const (
	maintenanceConsoleSupervisorArgument = "__aipermission_maintenance_console_supervisor"
	maintenanceSupervisorGracePeriod     = 150 * time.Millisecond
	maintenanceSupervisorKillPeriod      = 500 * time.Millisecond
)

// RunMaintenanceConsoleSupervisorIfRequested handles the private subprocess
// mode used to keep every maintenance-shell descendant under one subreaper.
func RunMaintenanceConsoleSupervisorIfRequested(arguments []string) (bool, int) {
	if len(arguments) == 0 || arguments[0] != maintenanceConsoleSupervisorArgument {
		return false, 0
	}
	if len(arguments) < 2 {
		fmt.Fprintln(os.Stderr, "maintenance console supervisor requires a shell")
		return true, 2
	}
	return true, runMaintenanceConsoleSupervisor(arguments[1], arguments[2:])
}
