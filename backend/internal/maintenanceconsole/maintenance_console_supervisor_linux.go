//go:build linux

package maintenanceconsole

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

type maintenanceProcessTreeReader func(int) (map[int]int, map[int]string, error)

func runMaintenanceConsoleSupervisor(shell string, arguments []string) int {
	return runMaintenanceSupervisor(shell, arguments, maintenanceSupervisorProcessTree)
}

func runMaintenanceSupervisor(shell string, arguments []string, read maintenanceProcessTreeReader) int {
	if _, _, err := read(os.Getpid()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := maintenancePIDHandlesAvailable(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
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
			if err := terminateMaintenanceSupervisorDescendants(os.Getpid(), read); err != nil {
				return 1
			}
			if err := reapMaintenanceSupervisorChildren(shellPID, read); err != nil {
				return 1
			}
			return maintenanceSupervisorExitCode(err)
		case received := <-signals:
			signalValue := received.(syscall.Signal)
			if signalValue == syscall.SIGCHLD {
				if err := reapMaintenanceSupervisorChildren(shellPID, read); err != nil {
					_ = terminateMaintenanceSupervisorDescendants(os.Getpid(), read)
					_ = command.Process.Kill()
					<-done
					return 1
				}
				continue
			}
			cleanupErr := terminateMaintenanceSupervisorDescendants(os.Getpid(), read)
			// Cleanup already consumed the shared grace; do not start a second wait.
			_ = command.Process.Kill()
			<-done
			if err := reapMaintenanceSupervisorChildren(shellPID, read); err != nil || cleanupErr != nil {
				return 1
			}
			return 128 + int(signalValue)
		}
	}
}

func terminateMaintenanceSupervisorDescendants(rootPID int, read maintenanceProcessTreeReader) error {
	return terminateMaintenanceDescendants(
		func() ([]int, error) { return maintenanceSupervisorDescendants(rootPID, read) },
		func(pids []int, grace, deadline time.Time) error {
			return signalMaintenanceDescendants(rootPID, pids, grace, deadline, read)
		},
	)
}

func terminateMaintenanceDescendants(read func() ([]int, error), signal func([]int, time.Time, time.Time) error) error {
	var failure error
	grace := time.Now().Add(maintenanceSupervisorGracePeriod)
	deadline := time.Now().Add(maintenanceSupervisorKillPeriod)
	for {
		pids, err := read()
		if err != nil {
			failure = errors.Join(failure, err)
		} else if len(pids) == 0 {
			return failure
		}
		// Only the current verified inventory may authorize a signal, never a cached PID.
		failure = errors.Join(failure, signal(pids, grace, deadline))
		if time.Now().After(deadline) {
			return errors.Join(failure, fmt.Errorf("maintenance console descendant cleanup could not be verified"))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func signalMaintenanceDescendants(rootPID int, pids []int, grace, deadline time.Time, read maintenanceProcessTreeReader) error {
	handles := make(map[int]int, len(pids))
	var failure error
	for _, pid := range pids {
		fd, err := unix.PidfdOpen(pid, 0)
		if err != nil {
			if !errors.Is(err, syscall.ESRCH) {
				failure = errors.Join(failure, err)
			}
			continue
		}
		handles[pid] = fd
		defer unix.Close(fd)
	}
	// One fresh inventory validates the entire bound batch, not one scan per PID.
	validated, scanErr := maintenanceSupervisorDescendants(rootPID, read)
	failure = errors.Join(failure, scanErr)
	for _, pid := range validated {
		fd, bound := handles[pid]
		if !bound {
			continue
		}
		// A slow validation must not skip the final signal to bound descendants.
		signal := syscall.SIGHUP
		if time.Now().After(grace) || time.Now().After(deadline) {
			signal = syscall.SIGKILL
		}
		if err := unix.PidfdSendSignal(fd, signal, nil, 0); err != nil && !errors.Is(err, syscall.ESRCH) {
			failure = errors.Join(failure, err)
		}
	}
	if time.Now().After(deadline) {
		failure = errors.Join(failure, errors.New("maintenance console signal deadline exceeded"))
	}
	return failure
}

func maintenanceSupervisorDescendants(rootPID int, read maintenanceProcessTreeReader) ([]int, error) {
	parents, states, err := read(rootPID)
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
	return result, err
}

func reapMaintenanceSupervisorChildren(shellPID int, read maintenanceProcessTreeReader) error {
	parents, _, err := read(os.Getpid())
	for pid, parentPID := range parents {
		if parentPID != os.Getpid() || pid == shellPID {
			continue
		}
		var status syscall.WaitStatus
		_, _ = syscall.Wait4(pid, &status, syscall.WNOHANG, nil)
	}
	return err
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
