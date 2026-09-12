//go:build !linux

package maintenanceconsole

func maintenanceConsoleProcessIsRunning(_ int) bool { return false }
