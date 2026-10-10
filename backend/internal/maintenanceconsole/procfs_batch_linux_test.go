//go:build linux

package maintenanceconsole

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestMaintenanceSlowInventoryBatchTeardown(t *testing.T) {
	if dir := os.Getenv("AIPERMISSION_TEST_BATCH_DIR"); dir != "" {
		ready := filepath.Join(dir, "ready")
		read := func(root int) (map[int]int, map[int]string, error) {
			if _, err := os.Stat(ready); err == nil {
				time.Sleep(65 * time.Millisecond)
			}
			return maintenanceSupervisorProcessTree(root)
		}
		script := fmt.Sprintf("trap '' HUP TERM; i=1; while [ $i -le 12 ]; do sh -c 'trap \"\" HUP TERM; echo $$ > \"$1\"; exec sleep 30' sh %q/$i & i=$((i+1)); done; i=1; while [ $i -le 12 ]; do while [ ! -s %q/$i ]; do sleep 0.01; done; i=$((i+1)); done; echo $$ > %q/shell; touch %q; exec sleep 30", dir, dir, dir, ready)
		os.Exit(runMaintenanceSupervisor("/bin/sh", []string{"-c", script}, read))
	}
	dir := t.TempDir()
	command := exec.Command(os.Args[0], "-test.run=^TestMaintenanceSlowInventoryBatchTeardown$")
	command.Env = append(os.Environ(), "AIPERMISSION_TEST_BATCH_DIR="+dir)
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	t.Cleanup(func() { _ = command.Process.Kill() })
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(dir, "ready")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("batch fixture startup timed out")
		}
		time.Sleep(10 * time.Millisecond)
	}
	names := []string{"shell"}
	for i := 1; i <= 12; i++ {
		names = append(names, strconv.Itoa(i))
	}
	pids := make([]int, 0, len(names))
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil {
			t.Fatal(err)
		}
		pids = append(pids, pid)
		t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	}
	terminateMaintenanceConsoleSupervisor(command.Process.Pid)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("supervisor did not terminate")
	}
	for _, pid := range pids {
		if maintenanceConsoleProcessIsRunning(pid) {
			t.Fatal("slow inventory exhausted the gateway grace before batch escalation")
		}
	}
}

func TestMaintenanceSlowValidationStillKillsBoundDescendants(t *testing.T) {
	child := exec.Command("sleep", "30")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- child.Wait() }()
	t.Cleanup(func() { _ = child.Process.Kill() })
	reads := 0
	read := func(root int) (map[int]int, map[int]string, error) {
		reads++
		time.Sleep(280 * time.Millisecond)
		return maintenanceSupervisorProcessTree(root)
	}
	err := terminateMaintenanceSupervisorDescendants(os.Getpid(), read)
	if err == nil || !strings.Contains(err.Error(), "could not be verified") {
		t.Fatalf("deadline exhaustion must remain visible: %v", err)
	}
	if reads != 2 {
		t.Fatalf("late teardown performed extra inventory scans: %d", reads)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("validated child survived the final batch after the signal deadline")
	}
}
