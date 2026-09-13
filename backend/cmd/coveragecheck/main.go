package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"io"
	"math"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type maintenancePolicy struct {
	BackendCoverageFloors           map[string]float64                  `json:"backendCoverageFloors"`
	BackendCoverageDefaultFloor     float64                             `json:"backendCoverageDefaultFloor"`
	BackendCoverageNeutralPackages  []string                            `json:"backendCoverageNeutralPackages"`
	BackendCoverageExcludedPackages map[string]coverageExclusion        `json:"backendCoverageExcludedPackages"`
	BackendCoveragePlatformFiles    map[string]platformCoverageEvidence `json:"backendCoveragePlatformFiles"`
}

type platformCoverageEvidence struct {
	Platform        string                `json:"platform"`
	BuildConstraint string                `json:"buildConstraint"`
	MinimumCoverage float64               `json:"minimumCoverage"`
	Tests           []runtimeTestEvidence `json:"tests"`
}

type runtimeTestEvidence struct {
	Package string `json:"package"`
	Name    string `json:"name"`
}

type coverageExclusion struct {
	Context string `json:"context"`
	Reason  string `json:"reason"`
}

type coveragePolicy struct {
	floors           map[string]float64
	defaultFloor     float64
	neutralPackages  map[string]bool
	excludedPackages map[string]coverageExclusion
	platformFiles    map[string]platformCoverageEvidence
}

type coverageCount struct {
	statements int64
	covered    int64
	files      map[string]bool
}

type productionFile struct {
	hasStatements   bool
	contexts        map[string]bool
	buildConstraint string
}

type productionPackages map[string]map[string]productionFile

type buildContext struct {
	label, goos, goarch, cgo string
	tags                     []string
}

var coveragePositionPattern = regexp.MustCompile(`^(.+\.go):(\d+)\.(\d+),(\d+)\.(\d+)$`)

const backendModulePath = "github.com/aipermission/aipermission/backend"

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
	for packagePath, exclusion := range policy.excludedPackages {
		files, ok := packages[packagePath]
		if !ok || !packageHasExecutableFiles(files) {
			failures = append(failures, fmt.Sprintf("%s: configured coverage exclusion is not executable production code", packagePath))
			continue
		}
		contexts := packageBuildContexts(files)
		if len(contexts) != 1 || !contexts[exclusion.Context] {
			failures = append(failures, fmt.Sprintf("%s: coverage exclusion must exist only in declared %s build context", packagePath, exclusion.Context))
		}
	}
	for sourcePath, evidence := range policy.platformFiles {
		packagePath := filepath.ToSlash(filepath.Dir(sourcePath))
		file := filepath.Base(sourcePath)
		productionFiles, packageExists := packages[packagePath]
		metadata, fileExists := productionFiles[file]
		if !packageExists || !fileExists || !metadata.hasStatements {
			failures = append(failures, fmt.Sprintf("%s: configured platform source is not executable production code", sourcePath))
		} else if metadata.buildConstraint != evidence.BuildConstraint {
			failures = append(failures, fmt.Sprintf("%s: platform coverage exemption declares build constraint %q, source uses %q", sourcePath, evidence.BuildConstraint, metadata.buildConstraint))
		}
	}
	for packagePath, count := range counts {
		if !productionPackagePath(packagePath) {
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
		_, excluded := policy.excludedPackages[packagePath]
		if policy.neutralPackages[packagePath] || excluded {
			continue
		}
		count, measured := counts[packagePath]
		if !measured || count.statements <= 0 {
			if packageUsesOnlyPlatformCoverageEvidence(packagePath, packages[packagePath], policy.platformFiles) {
				continue
			}
			failures = append(failures, fmt.Sprintf("%s: no coverage statements found", packagePath))
			continue
		}
		for file, metadata := range packages[packagePath] {
			sourcePath := packagePath + "/" + file
			_, platformExempt := policy.platformFiles[sourcePath]
			if metadata.hasStatements && !count.files[file] && !platformExempt {
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

func packageUsesOnlyPlatformCoverageEvidence(packagePath string, files map[string]productionFile, evidence map[string]platformCoverageEvidence) bool {
	hasExecutableFile := false
	for file, metadata := range files {
		if !metadata.hasStatements {
			continue
		}
		hasExecutableFile = true
		if _, ok := evidence[packagePath+"/"+file]; !ok {
			return false
		}
	}
	return hasExecutableFile
}

func packageHasExecutableFiles(files map[string]productionFile) bool {
	for _, metadata := range files {
		if metadata.hasStatements {
			return true
		}
	}
	return false
}

func packageBuildContexts(files map[string]productionFile) map[string]bool {
	contexts := map[string]bool{}
	for _, metadata := range files {
		for context := range metadata.contexts {
			contexts[context] = true
		}
	}
	return contexts
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
		if !productionPackagePath(packagePath) || floor < policy.BackendCoverageDefaultFloor || floor > 100 {
			return coveragePolicy{}, fmt.Errorf("invalid backend coverage floor %q: %.1f", packagePath, floor)
		}
	}
	neutral := make(map[string]bool, len(policy.BackendCoverageNeutralPackages))
	for _, packagePath := range policy.BackendCoverageNeutralPackages {
		if !productionPackagePath(packagePath) || neutral[packagePath] {
			return coveragePolicy{}, fmt.Errorf("invalid neutral backend coverage package %q", packagePath)
		}
		if _, ok := policy.BackendCoverageFloors[packagePath]; ok {
			return coveragePolicy{}, fmt.Errorf("backend coverage package %q is both floored and neutral", packagePath)
		}
		neutral[packagePath] = true
	}
	excluded := make(map[string]coverageExclusion, len(policy.BackendCoverageExcludedPackages))
	for packagePath, exclusion := range policy.BackendCoverageExcludedPackages {
		if !strings.HasPrefix(packagePath, "cmd/") || !productionPackagePath(packagePath) || neutral[packagePath] || exclusion.Context != "linux-e2e" || strings.TrimSpace(exclusion.Reason) == "" {
			return coveragePolicy{}, fmt.Errorf("invalid excluded backend coverage package %q", packagePath)
		}
		if _, ok := policy.BackendCoverageFloors[packagePath]; ok {
			return coveragePolicy{}, fmt.Errorf("backend coverage package %q is both floored and excluded", packagePath)
		}
		excluded[packagePath] = exclusion
	}
	platformFiles := make(map[string]platformCoverageEvidence, len(policy.BackendCoveragePlatformFiles))
	for configuredPath, evidence := range policy.BackendCoveragePlatformFiles {
		sourcePath := filepath.ToSlash(filepath.Clean(strings.TrimSpace(configuredPath)))
		expression, expressionErr := parseBuildConstraint(evidence.BuildConstraint)
		if configuredPath != sourcePath || !productionPackagePath(filepath.ToSlash(filepath.Dir(sourcePath))) || filepath.Ext(sourcePath) != ".go" || evidence.Platform != "windows" || expressionErr != nil || !buildConstraintMatchesPlatform(expression, evidence.Platform) || evidence.MinimumCoverage <= 0 || evidence.MinimumCoverage > 100 || len(evidence.Tests) == 0 {
			return coveragePolicy{}, fmt.Errorf("invalid backend coverage platform source %q", configuredPath)
		}
		for _, test := range evidence.Tests {
			if strings.TrimSpace(test.Package) == "" || strings.TrimSpace(test.Name) == "" {
				return coveragePolicy{}, fmt.Errorf("invalid backend coverage platform evidence %q", configuredPath)
			}
		}
		platformFiles[sourcePath] = evidence
	}
	return coveragePolicy{
		floors: policy.BackendCoverageFloors, defaultFloor: policy.BackendCoverageDefaultFloor,
		neutralPackages: neutral, excludedPackages: excluded, platformFiles: platformFiles,
	}, nil
}

func productionPackagePath(packagePath string) bool {
	return packagePath == path.Clean(packagePath) &&
		(strings.HasPrefix(packagePath, "internal/") || strings.HasPrefix(packagePath, "cmd/"))
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
	contexts := productionBuildContexts()
	inventories := make(map[string]productionPackages, len(contexts))
	for _, context := range contexts {
		inventory, err := listProductionPackagesFor(context)
		if err != nil {
			return nil, err
		}
		inventories[context.label] = inventory
	}
	merged := mergeProductionPackages(inventories)
	if err := requireCompleteProductionSourceInventory(".", merged); err != nil {
		return nil, err
	}
	return merged, nil
}

func requireCompleteProductionSourceInventory(root string, inventory productionPackages) error {
	unseen := []string{}
	for _, packageRoot := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, packageRoot), func(sourcePath string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("production source tree contains symlink %q", sourcePath)
			}
			if entry.IsDir() {
				if sourcePath != filepath.Join(root, packageRoot) && ignoredGoDirectory(entry.Name()) {
					if !strings.HasPrefix(entry.Name(), "_") || !inventoryContainsDirectory(root, sourcePath, inventory) {
						return filepath.SkipDir
					}
				}
				return nil
			}
			name := entry.Name()
			if filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
				return nil
			}
			if imported, err := importedTestdataPackage(sourcePath); err != nil {
				return err
			} else if imported != "" {
				return fmt.Errorf("production source imports test-only package %q", imported)
			}
			relative, err := filepath.Rel(root, sourcePath)
			if err != nil {
				return err
			}
			relative = filepath.ToSlash(relative)
			packagePath := filepath.ToSlash(filepath.Dir(relative))
			metadata, represented := inventory[packagePath][name]
			if !represented {
				unseen = append(unseen, relative)
				return nil
			}
			buildConstraint, err := productionFileBuildConstraint(sourcePath)
			if err != nil {
				return err
			}
			metadata.buildConstraint = buildConstraint
			inventory[packagePath][name] = metadata
			return nil
		})
		if err != nil {
			return fmt.Errorf("scan production sources: %w", err)
		}
	}
	if len(unseen) > 0 {
		sort.Strings(unseen)
		return fmt.Errorf("production sources are absent from every supported build context: %s", strings.Join(unseen, ", "))
	}
	return nil
}

func inventoryContainsDirectory(root, sourcePath string, inventory productionPackages) bool {
	relative, err := filepath.Rel(root, sourcePath)
	if err != nil {
		return false
	}
	directory := filepath.ToSlash(relative)
	for packagePath := range inventory {
		if packagePath == directory || strings.HasPrefix(packagePath, directory+"/") {
			return true
		}
	}
	return false
}

func productionFileBuildConstraint(sourcePath string) (string, error) {
	file, err := parser.ParseFile(token.NewFileSet(), sourcePath, nil, parser.PackageClauseOnly|parser.ParseComments)
	if err != nil {
		return "", err
	}

	var found string
	for _, group := range file.Comments {
		if group.Pos() > file.Package {
			break
		}
		for _, comment := range group.List {
			line := strings.TrimSpace(comment.Text)
			if !constraint.IsGoBuild(line) {
				continue
			}
			if found != "" {
				return "", fmt.Errorf("multiple //go:build constraints in %s", sourcePath)
			}
			expression, err := constraint.Parse(line)
			if err != nil {
				return "", fmt.Errorf("parse build constraint in %s: %w", sourcePath, err)
			}
			found = expression.String()
		}
	}
	return found, nil
}

func parseBuildConstraint(value string) (constraint.Expr, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, fmt.Errorf("empty build constraint")
	}
	return constraint.Parse("//go:build " + value)
}

func buildConstraintMatchesPlatform(expression constraint.Expr, platform string) bool {
	if expression == nil {
		return false
	}
	return expression.Eval(func(tag string) bool {
		return tag == platform || tag == "amd64" || tag == "gc"
	})
}

func importedTestdataPackage(sourcePath string) (string, error) {
	file, err := parser.ParseFile(token.NewFileSet(), sourcePath, nil, parser.ImportsOnly)
	if err != nil {
		return "", err
	}
	for _, specification := range file.Imports {
		packagePath, err := strconv.Unquote(specification.Path.Value)
		if err != nil {
			return "", err
		}
		if packagePath == "testdata" || strings.Contains(packagePath, "/testdata/") || strings.HasSuffix(packagePath, "/testdata") {
			return packagePath, nil
		}
	}
	return "", nil
}

func ignoredGoDirectory(name string) bool {
	return name == "testdata" || name == "vendor" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}

func productionBuildContexts() []buildContext {
	return []buildContext{
		{label: "host"},
		{label: "windows", goos: "windows", goarch: "amd64", cgo: "0"},
		{label: "linux-e2e", goos: "linux", goarch: "amd64", cgo: "1", tags: []string{"e2e"}},
	}
}

func listProductionPackagesFor(context buildContext) (productionPackages, error) {
	arguments := inventoryArguments(context)
	command := exec.Command("go", arguments...)
	if context.goos != "" {
		command.Env = inventoryEnvironment(os.Environ(), context)
	}
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("list %s production packages: %w\n%s", context.label, err, output)
	}
	backendRoot, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("resolve backend root: %w", err)
	}
	return readProductionPackagesAt(strings.NewReader(string(output)), backendRoot)
}

func inventoryArguments(context buildContext) []string {
	arguments := []string{"list", "-buildvcs=false", "-deps", "-json"}
	if len(context.tags) > 0 {
		arguments = append(arguments, "-tags="+strings.Join(context.tags, ","))
	}
	return append(arguments, "./internal/...", "./cmd/...")
}

func inventoryEnvironment(environment []string, context buildContext) []string {
	filtered := make([]string, 0, len(environment)+3)
	for _, entry := range environment {
		if strings.HasPrefix(entry, "GOOS=") || strings.HasPrefix(entry, "GOARCH=") || strings.HasPrefix(entry, "CGO_ENABLED=") {
			continue
		}
		filtered = append(filtered, entry)
	}
	return append(filtered, "GOOS="+context.goos, "GOARCH="+context.goarch, "CGO_ENABLED="+context.cgo)
}

func mergeProductionPackages(inventories map[string]productionPackages) productionPackages {
	merged := productionPackages{}
	for context, inventory := range inventories {
		for packagePath, files := range inventory {
			if merged[packagePath] == nil {
				merged[packagePath] = map[string]productionFile{}
			}
			for file, metadata := range files {
				current := merged[packagePath][file]
				current.hasStatements = current.hasStatements || metadata.hasStatements
				if current.buildConstraint == "" {
					current.buildConstraint = metadata.buildConstraint
				}
				if current.contexts == nil {
					current.contexts = map[string]bool{}
				}
				current.contexts[context] = true
				merged[packagePath][file] = current
			}
		}
	}
	return merged
}

func readProductionPackages(input io.Reader) (productionPackages, error) {
	return readProductionPackagesAt(input, "")
}

func readProductionPackagesAt(input io.Reader, backendRoot string) (productionPackages, error) {
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
		repositoryRoot := ""
		if backendRoot != "" {
			repositoryRoot = filepath.Dir(backendRoot)
		}
		repositoryLocal, err := directoryWithinRoot(item.Dir, repositoryRoot)
		if err != nil {
			return nil, fmt.Errorf("classify production package directory %q: %w", item.Dir, err)
		}
		backendLocal, err := directoryWithinRoot(item.Dir, backendRoot)
		if err != nil {
			return nil, fmt.Errorf("classify backend package directory %q: %w", item.Dir, err)
		}
		backendPackage := item.ImportPath == backendModulePath || strings.HasPrefix(item.ImportPath, backendModulePath+"/")
		if !backendPackage {
			if repositoryLocal {
				return nil, fmt.Errorf("repository-local production dependency %q uses an external module path", item.ImportPath)
			}
			continue
		}
		if backendRoot != "" && strings.TrimSpace(item.Dir) != "" && !backendLocal {
			return nil, fmt.Errorf("backend production package %q resolves outside the repository", item.ImportPath)
		}
		if len(item.GoFiles)+len(item.CgoFiles) == 0 {
			continue
		}
		packagePath := strings.TrimPrefix(strings.TrimPrefix(item.ImportPath, backendModulePath), "/")
		if !productionPackagePath(packagePath) {
			return nil, fmt.Errorf("invalid production package inventory entry %q", item.ImportPath)
		}
		if packagePath == "testdata" || strings.Contains(packagePath, "/testdata/") || strings.HasSuffix(packagePath, "/testdata") {
			return nil, fmt.Errorf("production inventory includes test-only package %q", item.ImportPath)
		}
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
			metadata := productionFile{hasStatements: hasStatements}
			if strings.TrimSpace(item.Dir) != "" {
				buildConstraint, err := productionFileBuildConstraint(filepath.Join(item.Dir, file))
				if err != nil {
					return nil, fmt.Errorf("inspect production source %s: %w", filepath.Join(item.Dir, file), err)
				}
				metadata.buildConstraint = buildConstraint
			}
			files[file] = metadata
		}
		packages[packagePath] = files
	}
	if len(packages) == 0 {
		return nil, fmt.Errorf("production package inventory is empty")
	}
	return packages, nil
}

func directoryWithinRoot(directory, root string) (bool, error) {
	if strings.TrimSpace(directory) == "" || strings.TrimSpace(root) == "" {
		return false, nil
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false, err
	}
	canonicalDirectory, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return false, err
	}
	relative, err := filepath.Rel(canonicalRoot, canonicalDirectory)
	if err != nil {
		return false, err
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))), nil
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
