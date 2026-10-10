//go:build linux

package maintenanceconsole

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"testing/fstest"
	"time"
)

func TestMaintenanceProcfsIdentityAndVisibility(t *testing.T) {
	for _, tc := range []struct {
		name, self, root, child string
		invalid                 bool
	}{
		{"valid", "12 (supervisor) S 1", "12 (supervisor) S 1", "13 (a ) child) S 12", false},
		{"foreign namespace", "39426 (supervisor) S 1", "12 (other) S 1", "13 (child) S 12", true},
		{"invalid self", "not a stat", "12 (supervisor) S 1", "", true},
		{"missing root", "12 (supervisor) S 1", "", "13 (child) S 12", true},
		{"malformed child", "12 (supervisor) S 1", "12 (supervisor) S 1", "broken", true},
		{"wrong child identity", "12 (supervisor) S 1", "12 (supervisor) S 1", "14 (child) S 12", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proc := fstest.MapFS{"self/stat": &fstest.MapFile{Data: []byte(tc.self)}}
			if tc.root != "" {
				proc["12/stat"] = &fstest.MapFile{Data: []byte(tc.root)}
			}
			if tc.child != "" {
				proc["13/stat"] = &fstest.MapFile{Data: []byte(tc.child)}
			}
			parents, _, err := maintenanceProcessTree(proc, 12)
			if tc.invalid {
				if err == nil {
					t.Fatal("unsafe procfs accepted")
				}
				return
			}
			if err != nil || parents[13] != 12 {
				t.Fatalf("parents=%v err=%v", parents, err)
			}
		})
	}
}

func TestMaintenanceCleanupRescansAfterInventoryFailure(t *testing.T) {
	child := exec.Command("sleep", "30")
	child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Process.Kill(); _ = child.Wait() })
	reads := 0
	err := terminateMaintenanceDescendants(func() ([]int, error) {
		reads++
		if reads == 1 {
			return nil, fs.ErrPermission
		}
		return maintenanceSupervisorDescendants(os.Getpid(), maintenanceSupervisorProcessTree)
	}, func(pids []int, grace, deadline time.Time) error {
		return signalMaintenanceDescendants(os.Getpid(), pids, grace, deadline, maintenanceSupervisorProcessTree)
	})
	if err == nil || reads < 3 {
		t.Fatalf("cleanup did not retain error and rescan: reads=%d err=%v", reads, err)
	}
	parents, states, scanErr := maintenanceSupervisorProcessTree(os.Getpid())
	if scanErr != nil || (parents[child.Process.Pid] == os.Getpid() && states[child.Process.Pid] != "Z") {
		t.Fatalf("detached child survived: parents=%v states=%v err=%v", parents, states, scanErr)
	}
}

func TestMaintenanceCleanupContinuesAfterSignalFailure(t *testing.T) {
	alive := map[int]bool{2: true, 3: true}
	attempts := map[int]int{}
	err := terminateMaintenanceDescendants(func() ([]int, error) {
		pids := []int{}
		for pid := range alive {
			pids = append(pids, pid)
		}
		return pids, nil
	}, func(pids []int, _, _ time.Time) error {
		var failure error
		for _, pid := range pids {
			attempts[pid]++
			if pid == 2 && attempts[pid] == 1 {
				failure = errors.Join(failure, fs.ErrPermission)
				continue
			}
			delete(alive, pid)
		}
		return failure
	})
	if err == nil || len(alive) != 0 || attempts[2] != 2 || attempts[3] != 1 {
		t.Fatalf("cleanup abandoned descendants: alive=%v attempts=%v err=%v", alive, attempts, err)
	}
}

type unreadableProcfs struct {
	fs.FS
	path string
	err  error
}

func (proc unreadableProcfs) Open(name string) (fs.File, error) {
	if name == proc.path {
		return nil, proc.err
	}
	return proc.FS.Open(name)
}

func TestMaintenanceProcfsReadFailuresAreNotEmptyTrees(t *testing.T) {
	proc := fstest.MapFS{
		"self/stat": &fstest.MapFile{Data: []byte("12 (supervisor) S 1")},
		"12/stat":   &fstest.MapFile{Data: []byte("12 (supervisor) S 1")},
		"13/stat":   &fstest.MapFile{Data: []byte("13 (child) S 12")},
	}
	for _, path := range []string{".", "self/stat", "12/stat", "13/stat"} {
		_, _, err := maintenanceProcessTree(unreadableProcfs{proc, path, fs.ErrPermission}, 12)
		if err == nil {
			t.Fatalf("permission failure at %s was treated as an empty tree", path)
		}
	}
	for _, missing := range []error{fs.ErrNotExist, syscall.ESRCH} {
		parents, _, err := maintenanceProcessTree(unreadableProcfs{proc, "13/stat", missing}, 12)
		if err != nil || len(parents) != 0 {
			t.Fatalf("disappearing child: %v %v", parents, err)
		}
		if _, _, err := maintenanceProcessTree(unreadableProcfs{proc, "12/stat", missing}, 12); err == nil {
			t.Fatal("missing supervisor accepted")
		}
	}
}
