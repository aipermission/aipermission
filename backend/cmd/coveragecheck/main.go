package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

var criticalCoverageFloors = map[string]float64{
	"internal/api":                   57.0,
	"internal/auditoutbox":           60.0,
	"internal/backups":               64.0,
	"internal/connectors/clickhouse": 58.0,
	"internal/connectors/docker":     52.0,
	"internal/connectors/kafka":      58.0,
	"internal/connectors/kubernetes": 36.0,
	"internal/connectors/mail":       53.0,
	"internal/connectors/postgres":   32.0,
	"internal/connectors/rabbitmq":   53.0,
	"internal/connectors/redis":      62.0,
	"internal/connectors/s3":         64.0,
	"internal/connectors/sqlsafe":    63.0,
	"internal/connectors/ssh":        76.0,
	"internal/connectortargets":      63.0,
	"internal/console":               72.0,
	"internal/db":                    75.0,
	"internal/filetransfer":          66.0,
	"internal/projectcapabilities":   74.0,
	"internal/projectvault":          69.0,
	"internal/restcontract":          84.0,
	"internal/sessionenv":            64.0,
	"internal/tokens":                82.0,
	"internal/vault":                 82.0,
	"internal/vaultrequests":         62.0,
	"internal/vaultsessions":         82.0,
}

type coverageCount struct {
	statements int64
	covered    int64
}

func main() {
	profilePath := flag.String("profile", "coverage.out", "Go coverage profile to check")
	flag.Parse()
	counts, err := readCoverageProfile(*profilePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	failed := false
	for _, packagePath := range sortedFloorPaths() {
		floor := criticalCoverageFloors[packagePath]
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

func sortedFloorPaths() []string {
	paths := make([]string, 0, len(criticalCoverageFloors))
	for path := range criticalCoverageFloors {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}
