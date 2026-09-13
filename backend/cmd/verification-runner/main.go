package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type testEntry struct {
	Package string `json:"package"`
	Name    string `json:"name"`
}

type policy struct {
	RecoveryTests             []testEntry `json:"recovery_tests"`
	ConnectorConformanceTests []string    `json:"connector_conformance_tests"`
	FuzzTargets               []testEntry `json:"fuzz_targets"`
}

type testEvent struct {
	Action  string `json:"Action"`
	Package string `json:"Package"`
	Test    string `json:"Test"`
	Output  string `json:"Output"`
}

func main() {
	if len(os.Args) != 2 || (os.Args[1] != "recovery" && os.Args[1] != "conformance" && os.Args[1] != "fuzz-inventory") {
		fatal(errors.New("usage: verification-runner recovery|conformance|fuzz-inventory"))
	}
	contents, err := os.ReadFile("../scripts/verification-policy.json")
	if err != nil {
		fatal(err)
	}
	var manifest policy
	if err := json.Unmarshal(contents, &manifest); err != nil {
		fatal(err)
	}
	entries := manifest.RecoveryTests
	prefix := "TestRecoveryDrill"
	if os.Args[1] == "fuzz-inventory" {
		if err := verifyInventory("fuzz", "Fuzz", manifest.FuzzTargets, true); err != nil {
			fatal(err)
		}
		return
	}
	if os.Args[1] == "conformance" {
		prefix = "Test"
		entries = make([]testEntry, 0, len(manifest.ConnectorConformanceTests))
		for _, name := range manifest.ConnectorConformanceTests {
			entries = append(entries, testEntry{Package: "./internal/connectors/conformance", Name: name})
		}
	}
	if err := verify(os.Args[1], prefix, entries); err != nil {
		fatal(err)
	}
}

func verify(mode, prefix string, entries []testEntry) error {
	if len(entries) == 0 {
		return fmt.Errorf("%s verification manifest is empty", mode)
	}
	packages, expected, err := inventory(entries)
	if err != nil {
		return err
	}
	if mode == "recovery" {
		if err := compareDeclaredInventory(mode, prefix, expected, true); err != nil {
			return err
		}
	} else {
		discoveryArgs := append([]string{"test", "-json"}, packages...)
		discoveryArgs = append(discoveryArgs, "-run", "^$", "-list", "^"+prefix)
		discovery, err := runGo(discoveryArgs...)
		if err != nil {
			return err
		}
		discovered, err := discoveredTests(discovery, prefix)
		if err != nil {
			return err
		}
		if strings.Join(discovered, "\n") != strings.Join(expected, "\n") {
			return fmt.Errorf("%s inventory drift: expected %v; discovered %v", mode, expected, discovered)
		}
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, regexp.QuoteMeta(entry.Name))
	}
	pattern := "^(" + strings.Join(names, "|") + ")$"
	runArgs := append([]string{"test"}, packages...)
	runArgs = append(runArgs, "-json", "-count=1", "-run", pattern)
	output, runErr := runGo(runArgs...)
	fmt.Print(output)
	if runErr != nil {
		return runErr
	}
	return verifyEvents(mode, output, expected)
}

func verifyInventory(mode, prefix string, entries []testEntry, rejectConstraints bool) error {
	if len(entries) == 0 {
		return fmt.Errorf("%s verification manifest is empty", mode)
	}
	_, expected, err := inventory(entries)
	if err != nil {
		return err
	}
	return compareDeclaredInventory(mode, prefix, expected, rejectConstraints)
}

func compareDeclaredInventory(mode, prefix string, expected []string, rejectConstraints bool) error {
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	discovered, err := declaredTests(root, prefix, rejectConstraints)
	if err != nil {
		return err
	}
	if strings.Join(discovered, "\n") != strings.Join(expected, "\n") {
		return fmt.Errorf("%s inventory drift: expected %v; discovered %v", mode, expected, discovered)
	}
	return nil
}

func declaredTests(root, prefix string, rejectConstraints bool) ([]string, error) {
	moduleName, err := moduleName(root)
	if err != nil {
		return nil, err
	}
	var result []string
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && (entry.Name() == "vendor" || strings.HasPrefix(entry.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, contents, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		var names []string
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if ok && function.Recv == nil && strings.HasPrefix(function.Name.Name, prefix) {
				names = append(names, function.Name.Name)
			}
		}
		if len(names) == 0 {
			return nil
		}
		if rejectConstraints && hasBuildConstraint(contents) {
			return fmt.Errorf("%s contains constrained %s declarations; verification inventory must be portable", path, prefix)
		}
		relative, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		packageName := moduleName
		if relative != "." {
			packageName += "/" + filepath.ToSlash(relative)
		}
		for _, name := range names {
			result = append(result, packageName+":"+name)
		}
		return nil
	})
	sort.Strings(result)
	return result, err
}

func moduleName(root string) (string, error) {
	contents, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(contents), "\n") {
		if fields := strings.Fields(line); len(fields) == 2 && fields[0] == "module" {
			return fields[1], nil
		}
	}
	return "", errors.New("go.mod has no module directive")
}

func hasBuildConstraint(contents []byte) bool {
	for _, line := range strings.Split(string(contents), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "package ") {
			return false
		}
		if strings.HasPrefix(trimmed, "//go:build ") || strings.HasPrefix(trimmed, "// +build ") {
			return true
		}
	}
	return false
}

func verifyEvents(mode, output string, expected []string) error {
	passed := map[string]bool{}
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		var event testEvent
		if json.Unmarshal(scanner.Bytes(), &event) != nil || event.Test == "" {
			continue
		}
		key := event.Package + ":" + event.Test
		if event.Action == "skip" {
			return fmt.Errorf("%s test skipped: %s", mode, key)
		}
		if event.Action == "pass" {
			passed[key] = true
		}
	}
	for _, name := range expected {
		if !passed[name] {
			return fmt.Errorf("%s test did not pass: %s", mode, name)
		}
	}
	return scanner.Err()
}

func inventory(entries []testEntry) ([]string, []string, error) {
	packageSet := map[string]bool{}
	expected := make([]string, 0, len(entries))
	for _, entry := range entries {
		packageSet[entry.Package] = true
		importPath, err := runGo("list", "-f", "{{.ImportPath}}", entry.Package)
		if err != nil {
			return nil, nil, err
		}
		expected = append(expected, strings.TrimSpace(importPath)+":"+entry.Name)
	}
	packages := make([]string, 0, len(packageSet))
	for packageName := range packageSet {
		packages = append(packages, packageName)
	}
	sort.Strings(packages)
	sort.Strings(expected)
	return packages, expected, nil
}

func discoveredTests(output, prefix string) ([]string, error) {
	var result []string
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		var event testEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return nil, fmt.Errorf("decode discovery event: %w", err)
		}
		name := strings.TrimSpace(event.Output)
		if strings.HasPrefix(name, prefix) && !strings.ContainsAny(name, " \t") {
			result = append(result, event.Package+":"+name)
		}
	}
	sort.Strings(result)
	return result, scanner.Err()
}

func runGo(arguments ...string) (string, error) {
	command := exec.Command("go", arguments...)
	command.Env = os.Environ()
	if root, err := moduleRoot(); err == nil {
		command.Dir = root
	} else {
		return "", err
	}
	return runCommand(command, "go "+strings.Join(arguments, " "))
}

func runCommand(command *exec.Cmd, display string) (string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	if err != nil {
		return stdout.String(), fmt.Errorf(
			"%s: %w\nstdout:\n%s\nstderr:\n%s",
			display,
			err,
			stdout.String(),
			stderr.String(),
		)
	}
	if stderr.Len() > 0 {
		_, _ = os.Stderr.Write(stderr.Bytes())
	}
	return stdout.String(), nil
}

func moduleRoot() (string, error) {
	directory, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory, nil
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return "", errors.New("Go module root was not found")
		}
		directory = parent
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
