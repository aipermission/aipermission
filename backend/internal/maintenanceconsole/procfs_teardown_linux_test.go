//go:build linux

package maintenanceconsole

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestMaintenancePersistentInventoryFailure(t *testing.T) {
	if marker := os.Getenv("AIPERMISSION_TEST_PROCFS_FAILURE"); marker != "" {
		read := func(root int) (map[int]int, map[int]string, error) {
			proc := fs.FS(os.DirFS("/proc"))
			if _, err := os.Stat(marker); err == nil {
				path := "1/stat"
				if os.Getenv("AIPERMISSION_TEST_UNREADABLE_SHELL") != "" {
					data, _ := os.ReadFile(marker)
					path = strings.TrimSpace(string(data)) + "/stat"
				}
				proc = unreadableProcfs{proc, path, fs.ErrPermission}
			}
			return maintenanceProcessTree(proc, root)
		}
		os.Exit(runMaintenanceSupervisor("/bin/sh", []string{"-c", os.Getenv("AIPERMISSION_TEST_PROCFS_SCRIPT")}, read))
	}
	dir := t.TempDir()
	marker, childFile := filepath.Join(dir, "failure"), filepath.Join(dir, "child")
	script := fmt.Sprintf("setsid sh -c 'trap \"\" HUP TERM; echo $$ > %q; exec sleep 30' </dev/null >/dev/null 2>&1 & while [ ! -s %q ]; do sleep 0.01; done; touch %q; exec sleep 30", childFile, childFile, marker)
	command := exec.Command(os.Args[0], "-test.run=^TestMaintenancePersistentInventoryFailure$")
	command.Env = append(os.Environ(), "AIPERMISSION_TEST_PROCFS_FAILURE="+marker, "AIPERMISSION_TEST_PROCFS_SCRIPT="+script)
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	t.Cleanup(func() { _ = command.Process.Kill() })
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fixture startup timeout")
		}
		time.Sleep(10 * time.Millisecond)
	}
	data, err := os.ReadFile(childFile)
	if err != nil {
		t.Fatal(err)
	}
	child, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(child, syscall.SIGKILL) })
	_ = command.Process.Signal(syscall.SIGHUP)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("partial cleanup was reported as verified success")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("supervisor did not exit within teardown deadline")
	}
	if maintenanceConsoleProcessIsRunning(child) {
		t.Fatal("readable detached child survived a persistent unrelated procfs failure")
	}
}

func TestMaintenanceUnreadableShellUsesOneTeardownGrace(t *testing.T) {
	childFile := filepath.Join(t.TempDir(), "shell")
	script := fmt.Sprintf("trap '' HUP TERM; echo $$ > %q; exec sleep 30", childFile)
	command := exec.Command(os.Args[0], "-test.run=^TestMaintenancePersistentInventoryFailure$")
	command.Env = append(os.Environ(), "AIPERMISSION_TEST_PROCFS_FAILURE="+childFile,
		"AIPERMISSION_TEST_PROCFS_SCRIPT="+script, "AIPERMISSION_TEST_UNREADABLE_SHELL=1")
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	t.Cleanup(func() { _ = command.Process.Kill() })
	deadline := time.Now().Add(3 * time.Second)
	var child int
	for {
		data, err := os.ReadFile(childFile)
		if err == nil {
			child, err = strconv.Atoi(strings.TrimSpace(string(data)))
			if err == nil {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("fixture startup timed out")
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Cleanup(func() { _ = syscall.Kill(child, syscall.SIGKILL) })
	terminateMaintenanceConsoleSupervisor(command.Process.Pid)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("supervisor did not terminate")
	}
	if maintenanceConsoleProcessIsRunning(child) {
		t.Fatal("shell survived gateway termination after an unreadable shell inventory")
	}
}

func TestMaintenanceCleanupDoesNotSignalDisprovedPIDs(t *testing.T) {
	reads, signals := 0, 0
	err := terminateMaintenanceDescendants(func() ([]int, error) {
		reads++
		if reads == 1 {
			return []int{101}, nil
		}
		if reads == 4 {
			return nil, nil
		}
		return nil, fs.ErrPermission
	}, func(pids []int, _, _ time.Time) error {
		signals += len(pids)
		return nil
	})
	if err == nil || signals != 1 {
		t.Fatalf("cached PID received further signals: count=%d err=%v", signals, err)
	}
}

func TestMaintenanceSignalRevalidatesBoundProcessAncestry(t *testing.T) {
	child := exec.Command("sleep", "30")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Process.Kill(); _ = child.Wait() })
	read := func(int) (map[int]int, map[int]string, error) {
		return map[int]int{child.Process.Pid: 1}, map[int]string{child.Process.Pid: "S"}, nil
	}
	if err := signalMaintenanceDescendants(os.Getpid(), []int{child.Process.Pid}, time.Now().Add(-time.Second), time.Now().Add(time.Second), read); err != nil {
		t.Fatalf("ancestry revalidation failed: %v", err)
	}
	if !maintenanceConsoleProcessIsRunning(child.Process.Pid) {
		t.Fatal("process with disproved ancestry was signaled")
	}
}

func TestMaintenancePartialInventoryRequiresVerifiedRoot(t *testing.T) {
	proc := unreadableProcfs{os.DirFS("/proc"), "1/stat", fs.ErrPermission}
	parents, _, err := maintenanceProcessTree(proc, os.Getpid())
	if err == nil || parents == nil {
		t.Fatalf("partial tree discarded or silently accepted: %v %v", parents, err)
	}
	parents, _, err = maintenanceProcessTree(unreadableProcfs{proc, "self/stat", fs.ErrPermission}, os.Getpid())
	if err == nil || parents != nil {
		t.Fatal("unverified namespace returned usable process identities")
	}
}
