package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type maintenancePolicy struct {
	BackendCoverageFloors          map[string]float64 `json:"backendCoverageFloors"`
	BackendCoverageDefaultFloor    float64            `json:"backendCoverageDefaultFloor"`
	BackendCoverageNeutralPackages []string           `json:"backendCoverageNeutralPackages"`
}

type coveragePolicy struct {
	floors          map[string]float64
	defaultFloor    float64
	neutralPackages map[string]bool
}

type coverageCount struct {
	statements int64
	covered    int64
	files      map[string]bool
}

type productionFile struct {
	hasStatements bool
}

type productionPackages map[string]map[string]productionFile

var coveragePositionPattern = regexp.MustCompile(`^(.+\.go):(\d+)\.(\d+),(\d+)\.(\d+)$`)

func main() {
	profilePath := flag.String("profile", "coverage.out", "Go coverage profile to check")
	policyPath := flag.String("policy", "../maintenance-policy.json", "maintenance policy containing backend coverage floors")
	flag.Parse()
	policy, err := readCoveragePolicy(*policyPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	counts, err := readCoverageProfile(*profilePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	packages, err := listProductionPackages()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if failures := checkCoverage(policy, counts, packages, os.Stdout); len(failures) > 0 {
		for _, failure := range failures {
			fmt.Fprintln(os.Stderr, failure)
		}
		fmt.Fprintln(os.Stderr, "critical backend coverage floor failed")
		os.Exit(1)
	}
}

func checkCoverage(policy coveragePolicy, counts map[string]coverageCount, packages productionPackages, output io.Writer) []string {
	failures := []string{}
	for packagePath := range policy.floors {
		if _, ok := packages[packagePath]; !ok {
			failures = append(failures, fmt.Sprintf("%s: configured coverage package is not in the production build", packagePath))
		}
	}
	for packagePath := range policy.neutralPackages {
		files, ok := packages[packagePath]
		if !ok {
			failures = append(failures, fmt.Sprintf("%s: configured neutral package is not in the production build", packagePath))
			continue
		}
		for file, metadata := range files {
			if metadata.hasStatements {
				failures = append(failures, fmt.Sprintf("%s/%s: configured neutral package contains executable statements", packagePath, file))
			}
		}
	}
	for packagePath, count := range counts {
		if !strings.HasPrefix(packagePath, "internal/") {
			continue
		}
		productionFiles, exists := packages[packagePath]
		if !exists {
			failures = append(failures, fmt.Sprintf("%s: coverage profile contains a package outside the production build", packagePath))
			continue
		}
		for file := range count.files {
			if _, exists := productionFiles[file]; !exists {
				failures = append(failures, fmt.Sprintf("%s/%s: coverage profile contains a file outside the production build", packagePath, file))
			}
		}
	}
	for _, packagePath := range sortedPackagePaths(packages) {
		if policy.neutralPackages[packagePath] {
			continue
		}
		count, measured := counts[packagePath]
		if !measured || count.statements <= 0 {
			failures = append(failures, fmt.Sprintf("%s: no coverage statements found", packagePath))
			continue
		}
		for file, metadata := range packages[packagePath] {
			if metadata.hasStatements && !count.files[file] {
				failures = append(failures, fmt.Sprintf("%s/%s: executable production file has no coverage measurements", packagePath, file))
			}
		}
		floor := policy.defaultFloor
		if explicit, ok := policy.floors[packagePath]; ok {
			floor = explicit
		}
		percent := float64(count.covered) * 100 / float64(count.statements)
		fmt.Fprintf(output, "%-32s %5.1f%% (floor %.1f%%)\n", packagePath, percent, floor)
		if percent < floor {
			failures = append(failures, fmt.Sprintf("%s: %.1f%% coverage is below %.1f%% floor", packagePath, percent, floor))
		}
	}
	sort.Strings(failures)
	return failures
}

func readCoveragePolicy(path string) (coveragePolicy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return coveragePolicy{}, fmt.Errorf("read maintenance policy: %w", err)
	}
	var policy maintenancePolicy
	if err := json.Unmarshal(data, &policy); err != nil {
		return coveragePolicy{}, fmt.Errorf("decode maintenance policy: %w", err)
	}
	if len(policy.BackendCoverageFloors) == 0 {
		return coveragePolicy{}, fmt.Errorf("maintenance policy defines no backend coverage floors")
	}
	if policy.BackendCoverageDefaultFloor <= 0 || policy.BackendCoverageDefaultFloor > 100 {
		return coveragePolicy{}, fmt.Errorf("invalid default backend coverage floor: %.1f", policy.BackendCoverageDefaultFloor)
	}
	for packagePath, floor := range policy.BackendCoverageFloors {
		if !strings.HasPrefix(packagePath, "internal/") || floor < policy.BackendCoverageDefaultFloor || floor > 100 {
			return coveragePolicy{}, fmt.Errorf("invalid backend coverage floor %q: %.1f", packagePath, floor)
		}
	}
	neutral := make(map[string]bool, len(policy.BackendCoverageNeutralPackages))
	for _, packagePath := range policy.BackendCoverageNeutralPackages {
		if !strings.HasPrefix(packagePath, "internal/") || neutral[packagePath] {
			return coveragePolicy{}, fmt.Errorf("invalid neutral backend coverage package %q", packagePath)
		}
		if _, ok := policy.BackendCoverageFloors[packagePath]; ok {
			return coveragePolicy{}, fmt.Errorf("backend coverage package %q is both floored and neutral", packagePath)
		}
		neutral[packagePath] = true
	}
	return coveragePolicy{floors: policy.BackendCoverageFloors, defaultFloor: policy.BackendCoverageDefaultFloor, neutralPackages: neutral}, nil
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
	mode := ""
	lines := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if first {
			first = false
			mode = strings.TrimPrefix(line, "mode: ")
			if mode == line || (mode != "set" && mode != "count" && mode != "atomic") {
				return nil, fmt.Errorf("invalid coverage profile header")
			}
			continue
		}
		lines++
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
		if mode == "set" && count > 1 {
			return nil, fmt.Errorf("invalid set execution count in %q", line)
		}
		packagePath, sourceFile, err := coveragePackage(fields[0])
		if err != nil {
			return nil, err
		}
		current := counts[packagePath]
		if current.files == nil {
			current.files = map[string]bool{}
		}
		current.files[sourceFile] = true
		if current.statements > math.MaxInt64-statements || (count > 0 && current.covered > math.MaxInt64-statements) {
			return nil, fmt.Errorf("coverage statement count overflow in %q", line)
		}
		current.statements += statements
		if count > 0 {
			current.covered += statements
		}
		counts[packagePath] = current
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read coverage profile: %w", err)
	}
	if first || lines == 0 {
		return nil, fmt.Errorf("coverage profile contains no statements")
	}
	return counts, nil
}

func coveragePackage(position string) (string, string, error) {
	match := coveragePositionPattern.FindStringSubmatch(position)
	if match == nil {
		return "", "", fmt.Errorf("invalid coverage position %q", position)
	}
	coordinates := make([]uint64, 0, 4)
	for _, value := range match[2:] {
		parsed, err := strconv.ParseUint(value, 10, 64)
		if err != nil || parsed == 0 {
			return "", "", fmt.Errorf("invalid coverage position %q", position)
		}
		coordinates = append(coordinates, parsed)
	}
	if coordinates[0] > coordinates[2] || (coordinates[0] == coordinates[2] && coordinates[1] > coordinates[3]) {
		return "", "", fmt.Errorf("invalid coverage position %q", position)
	}
	file := filepath.ToSlash(match[1])
	marker := "/backend/"
	if index := strings.Index(file, marker); index >= 0 {
		file = file[index+len(marker):]
	} else if strings.HasPrefix(file, "backend/") {
		file = strings.TrimPrefix(file, "backend/")
	} else {
		return "", "", fmt.Errorf("coverage position is outside the backend module: %q", position)
	}
	packagePath := filepath.ToSlash(filepath.Dir(file))
	if !strings.HasPrefix(packagePath, "internal/") && !strings.HasPrefix(packagePath, "cmd/") {
		return "", "", fmt.Errorf("coverage position has an unsupported package: %q", position)
	}
	return packagePath, filepath.Base(file), nil
}

func listProductionPackages() (productionPackages, error) {
	command := exec.Command("go", "list", "-json", "./internal/...")
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("list production packages: %w", err)
	}
	return readProductionPackages(strings.NewReader(string(output)))
}

func readProductionPackages(input io.Reader) (productionPackages, error) {
	decoder := json.NewDecoder(input)
	packages := productionPackages{}
	for {
		var item struct {
			ImportPath string
			Dir        string
			GoFiles    []string
			CgoFiles   []string
		}
		if err := decoder.Decode(&item); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("decode production package inventory: %w", err)
		}
		marker := "/backend/internal/"
		index := strings.Index(item.ImportPath, marker)
		if index < 0 {
			return nil, fmt.Errorf("invalid production package inventory entry %q", item.ImportPath)
		}
		if len(item.GoFiles)+len(item.CgoFiles) == 0 {
			continue
		}
		packagePath := "internal/" + item.ImportPath[index+len(marker):]
		if _, exists := packages[packagePath]; exists {
			return nil, fmt.Errorf("duplicate production package inventory entry %q", item.ImportPath)
		}
		files := map[string]productionFile{}
		for _, file := range append(item.GoFiles, item.CgoFiles...) {
			hasStatements := true
			if strings.TrimSpace(item.Dir) != "" {
				var err error
				hasStatements, err = productionFileHasStatements(filepath.Join(item.Dir, file))
				if err != nil {
					return nil, fmt.Errorf("inspect production source %s: %w", filepath.Join(item.Dir, file), err)
				}
			}
			files[file] = productionFile{hasStatements: hasStatements}
		}
		packages[packagePath] = files
	}
	if len(packages) == 0 {
		return nil, fmt.Errorf("production package inventory is empty")
	}
	return packages, nil
}

func productionFileHasStatements(path string) (bool, error) {
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		return false, err
	}
	hasStatements := false
	ast.Inspect(parsed, func(node ast.Node) bool {
		if hasStatements {
			return false
		}
		switch typed := node.(type) {
		case *ast.FuncDecl:
			hasStatements = typed.Body != nil
		case *ast.FuncLit:
			hasStatements = typed.Body != nil
		case *ast.GenDecl:
			if typed.Tok == token.VAR {
				hasStatements = declarationCallsFunction(typed)
			}
		}
		return !hasStatements
	})
	return hasStatements, nil
}

func declarationCallsFunction(declaration *ast.GenDecl) bool {
	called := false
	for _, specification := range declaration.Specs {
		value, ok := specification.(*ast.ValueSpec)
		if !ok {
			continue
		}
		for _, expression := range value.Values {
			ast.Inspect(expression, func(node ast.Node) bool {
				if _, ok := node.(*ast.CallExpr); ok {
					called = true
					return false
				}
				return !called
			})
			if called {
				return true
			}
		}
	}
	return false
}

func sortedPackagePaths(packages productionPackages) []string {
	paths := make([]string, 0, len(packages))
	for path := range packages {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}
