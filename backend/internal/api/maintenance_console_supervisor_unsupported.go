//go:build !linux

package api

import (
	"fmt"
	"os"
)

func runMaintenanceConsoleSupervisor(_ string, _ []string) int {
	fmt.Fprintln(os.Stderr, "maintenance console supervisor is supported only on Linux")
	return 1
}
