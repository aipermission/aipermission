//go:build linux

package api

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func runMaintenanceConsoleSupervisor(shell string, arguments []string) int {
	if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0); err != nil {
		fmt.Fprintf(os.Stderr, "maintenance console supervisor unavailable: %v\n", err)
		return 1
	}

	signals := make(chan os.Signal, 8)
	signal.Notify(signals, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGCHLD)
	defer signal.Stop(signals)

	command := exec.Command(shell, arguments...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	command.Env = os.Environ()
	if err := command.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "maintenance console shell failed to start: %v\n", err)
		return 1
	}
	shellPID := command.Process.Pid
	// The shell owns the PTY after Start. Keeping supervisor duplicates open
	// would hide shell-side EOF when a command redirects or closes the terminal.
	_ = os.Stdin.Close()
	_ = os.Stdout.Close()
	_ = os.Stderr.Close()

	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	for {
		select {
		case err := <-done:
			terminateMaintenanceSupervisorDescendants(os.Getpid())
			reapMaintenanceSupervisorChildren(shellPID)
			return maintenanceSupervisorExitCode(err)
		case received := <-signals:
			signalValue := received.(syscall.Signal)
			if signalValue == syscall.SIGCHLD {
				reapMaintenanceSupervisorChildren(shellPID)
				continue
			}
			terminateMaintenanceSupervisorDescendants(os.Getpid())
			select {
			case <-done:
			case <-time.After(maintenanceSupervisorKillPeriod):
				_ = command.Process.Kill()
				<-done
			}
			reapMaintenanceSupervisorChildren(shellPID)
			return 128 + int(signalValue)
		}
	}
}

func terminateMaintenanceSupervisorDescendants(rootPID int) {
	signalMaintenanceSupervisorDescendants(rootPID, syscall.SIGHUP)
	if waitForMaintenanceSupervisorDescendants(rootPID, maintenanceSupervisorGracePeriod) {
		return
	}
	deadline := time.Now().Add(maintenanceSupervisorKillPeriod)
	for {
		signalMaintenanceSupervisorDescendants(rootPID, syscall.SIGKILL)
		if waitForMaintenanceSupervisorDescendants(rootPID, 10*time.Millisecond) || time.Now().After(deadline) {
			return
		}
	}
}

func signalMaintenanceSupervisorDescendants(rootPID int, signal syscall.Signal) {
	for _, pid := range maintenanceSupervisorDescendants(rootPID) {
		_ = syscall.Kill(pid, signal)
	}
}

func waitForMaintenanceSupervisorDescendants(rootPID int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for len(maintenanceSupervisorDescendants(rootPID)) > 0 {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
	return true
}

func maintenanceSupervisorDescendants(rootPID int) []int {
	parents, states := maintenanceSupervisorProcessTree(rootPID)
	descendants := map[int]struct{}{rootPID: {}}
	result := make([]int, 0, 4)
	for changed := true; changed; {
		changed = false
		for pid, parentPID := range parents {
			if _, known := descendants[pid]; known {
				continue
			}
			if _, parentKnown := descendants[parentPID]; parentKnown {
				descendants[pid] = struct{}{}
				if states[pid] != "Z" {
					result = append(result, pid)
				}
				changed = true
			}
		}
	}
	return result
}

func maintenanceSupervisorProcessTree(rootPID int) (map[int]int, map[int]string) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, nil
	}
	parents := make(map[int]int, len(entries))
	states := make(map[int]string, len(entries))
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid < 1 || pid == rootPID {
			continue
		}
		stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if err != nil {
			continue
		}
		closingParen := strings.LastIndexByte(string(stat), ')')
		if closingParen < 0 {
			continue
		}
		fields := strings.Fields(string(stat[closingParen+1:]))
		if len(fields) < 2 {
			continue
		}
		parentPID, err := strconv.Atoi(fields[1])
		if err == nil {
			parents[pid] = parentPID
			states[pid] = fields[0]
		}
	}
	return parents, states
}

func reapMaintenanceSupervisorChildren(shellPID int) {
	parents, _ := maintenanceSupervisorProcessTree(os.Getpid())
	for pid, parentPID := range parents {
		if parentPID != os.Getpid() || pid == shellPID {
			continue
		}
		var status syscall.WaitStatus
		_, _ = syscall.Wait4(pid, &status, syscall.WNOHANG, nil)
	}
}

func maintenanceSupervisorExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return exitError.ExitCode()
	}
	return 1
}
