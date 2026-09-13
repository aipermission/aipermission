//go:build !linux

package maintenanceconsole

import (
	"strings"
	"testing"
)

func maintenanceConsoleProcessIsRunning(_ int) bool { return false }

func TestMaintenanceConsoleUnsupportedRuntimeFailsClosed(t *testing.T) {
	if Supported() {
		t.Fatal("unsupported platform reported maintenance console support")
	}
	session, err := startMaintenanceConsoleSession()
	if err == nil || !strings.Contains(err.Error(), "supported only on Linux") {
		if session != nil {
			session.Close()
		}
		t.Fatalf("unsupported maintenance console error = %v", err)
	}
	terminateMaintenanceConsoleSupervisor(1)
	if maintenanceConsoleProcessExists(1) {
		t.Fatal("unsupported platform reported a maintenance console process")
	}
}

func TestMaintenanceConsoleUnsupportedSupervisorFailsClosed(t *testing.T) {
	if exitCode := runMaintenanceConsoleSupervisor("", nil); exitCode != 1 {
		t.Fatalf("unsupported supervisor exit code = %d, want 1", exitCode)
	}
}
