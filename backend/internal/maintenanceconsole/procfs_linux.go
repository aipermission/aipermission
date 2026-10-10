//go:build linux

package maintenanceconsole

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"syscall"
)

func maintenanceSupervisorProcessTree(rootPID int) (map[int]int, map[int]string, error) {
	return maintenanceProcessTree(os.DirFS("/proc"), rootPID)
}

func maintenanceProcessTree(proc fs.FS, rootPID int) (map[int]int, map[int]string, error) {
	self, err := fs.ReadFile(proc, "self/stat")
	if err != nil {
		return nil, nil, fmt.Errorf("maintenance console procfs identity unavailable: %w", err)
	}
	pid, _, _, err := maintenanceProcessStat(self)
	if err != nil || pid != rootPID {
		return nil, nil, fmt.Errorf("maintenance console requires procfs in the current PID namespace")
	}
	entries, err := fs.ReadDir(proc, ".")
	if err != nil {
		return nil, nil, fmt.Errorf("maintenance console process inventory unavailable: %w", err)
	}
	parents, states := make(map[int]int, len(entries)), make(map[int]string, len(entries))
	rootSeen := false
	var failure error
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid < 1 {
			continue
		}
		stat, err := fs.ReadFile(proc, entry.Name()+"/stat")
		if (errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ESRCH)) && pid != rootPID {
			continue
		}
		if err != nil {
			failure = errors.Join(failure, fmt.Errorf("maintenance console process metadata unavailable: %w", err))
			continue
		}
		statPID, parent, state, err := maintenanceProcessStat(stat)
		if err != nil || statPID != pid {
			failure = errors.Join(failure, fmt.Errorf("maintenance console process metadata is invalid"))
			continue
		}
		if pid == rootPID {
			rootSeen = true
			continue
		}
		parents[pid], states[pid] = parent, state
	}
	if !rootSeen {
		return nil, nil, fmt.Errorf("maintenance console supervisor is not visible in procfs")
	}
	return parents, states, failure
}

func maintenanceProcessStat(stat []byte) (int, int, string, error) {
	value := string(stat)
	opening, closing := strings.IndexByte(value, '('), strings.LastIndexByte(value, ')')
	if opening < 1 || closing <= opening {
		return 0, 0, "", errors.New("invalid process stat")
	}
	pid, pidErr := strconv.Atoi(strings.TrimSpace(value[:opening]))
	fields := strings.Fields(value[closing+1:])
	if len(fields) < 2 {
		return 0, 0, "", errors.New("incomplete process stat")
	}
	parent, parentErr := strconv.Atoi(fields[1])
	if pidErr != nil || parentErr != nil || pid < 1 || parent < 0 || len(fields[0]) != 1 {
		return 0, 0, "", errors.New("invalid process identity")
	}
	return pid, parent, fields[0], nil
}
