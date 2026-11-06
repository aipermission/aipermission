//go:build linux

package maintenanceconsole

import (
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestSupervisorTerminationRetainsOriginalProcessIdentity(t *testing.T) {
	for _, reaped := range []bool{false, true} {
		t.Run(map[bool]string{false: "running", true: "reaped"}[reaped], func(t *testing.T) {
			survivor := exec.Command("sleep", "30")
			if err := survivor.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = survivor.Process.Kill(); _ = survivor.Wait() })
			original := exec.Command("sleep", "30")
			if err := original.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = original.Process.Kill(); _ = original.Wait() })
			if reaped {
				_ = original.Process.Kill()
				_ = original.Wait()
			}
			// A stale/reused numeric identity must not override the exec handle.
			originalPID := original.Process.Pid
			original.Process.Pid = survivor.Process.Pid
			started := time.Now()
			terminateMaintenanceConsoleSupervisor(original.Process)
			original.Process.Pid = originalPID
			if err := survivor.Process.Signal(syscall.Signal(0)); err != nil {
				t.Fatalf("unrelated process was signaled through a stale numeric PID: %v", err)
			}
			if reaped && time.Since(started) > 250*time.Millisecond {
				t.Fatal("termination polled an unrelated reused numeric PID")
			}
		})
	}
	terminateMaintenanceConsoleSupervisor(nil)
}
