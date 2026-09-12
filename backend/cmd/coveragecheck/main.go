package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type maintenancePolicy struct {
	BackendCoverageFloors map[string]float64 `json:"backendCoverageFloors"`
}

type coverageCount struct {
	statements int64
	covered    int64
}

func main() {
	profilePath := flag.String("profile", "coverage.out", "Go coverage profile to check")
	policyPath := flag.String("policy", "../maintenance-policy.json", "maintenance policy containing backend coverage floors")
	flag.Parse()
	floors, err := readCoverageFloors(*policyPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	counts, err := readCoverageProfile(*profilePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	failed := false
	for _, packagePath := range sortedFloorPaths(floors) {
		floor := floors[packagePath]
		count, ok := counts[packagePath]
		if !ok || count.statements == 0 {
			fmt.Fprintf(os.Stderr, "%s: no coverage statements found\n", packagePath)
			failed = true
			continue
		}
		percent := float64(count.covered) * 100 / float64(count.statements)
		fmt.Printf("%-32s %5.1f%% (floor %.1f%%)\n", packagePath, percent, floor)
		if percent+0.0001 < floor {
			failed = true
		}
	}
	if failed {
		fmt.Fprintln(os.Stderr, "critical backend coverage floor failed")
		os.Exit(1)
	}
}

func readCoverageFloors(path string) (map[string]float64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read maintenance policy: %w", err)
	}
	var policy maintenancePolicy
	if err := json.Unmarshal(data, &policy); err != nil {
		return nil, fmt.Errorf("decode maintenance policy: %w", err)
	}
	if len(policy.BackendCoverageFloors) == 0 {
		return nil, fmt.Errorf("maintenance policy defines no backend coverage floors")
	}
	for packagePath, floor := range policy.BackendCoverageFloors {
		if !strings.HasPrefix(packagePath, "internal/") || floor <= 0 || floor > 100 {
			return nil, fmt.Errorf("invalid backend coverage floor %q: %.1f", packagePath, floor)
		}
	}
	return policy.BackendCoverageFloors, nil
}

func readCoverageProfile(path string) (map[string]coverageCount, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open coverage profile: %w", err)
	}
	defer file.Close()
	counts := map[string]coverageCount{}
	scanner := bufio.NewScanner(file)
	first := true
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if first {
			first = false
			if !strings.HasPrefix(line, "mode:") {
				return nil, fmt.Errorf("invalid coverage profile header")
			}
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 {
			return nil, fmt.Errorf("invalid coverage profile line %q", line)
		}
		statements, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || statements < 0 {
			return nil, fmt.Errorf("invalid statement count in %q", line)
		}
		count, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil || count < 0 {
			return nil, fmt.Errorf("invalid execution count in %q", line)
		}
		packagePath := coveragePackage(fields[0])
		current := counts[packagePath]
		current.statements += statements
		if count > 0 {
			current.covered += statements
		}
		counts[packagePath] = current
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read coverage profile: %w", err)
	}
	return counts, nil
}

func coveragePackage(position string) string {
	file := strings.SplitN(position, ":", 2)[0]
	marker := "/backend/"
	if index := strings.Index(file, marker); index >= 0 {
		file = file[index+len(marker):]
	} else if index := strings.Index(file, "/aipermission/"); index >= 0 {
		file = file[index+len("/aipermission/"):]
	}
	return filepath.ToSlash(filepath.Dir(file))
}

func sortedFloorPaths(floors map[string]float64) []string {
	paths := make([]string, 0, len(floors))
	for path := range floors {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}
