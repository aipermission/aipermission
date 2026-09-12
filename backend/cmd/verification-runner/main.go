package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
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
}

type testEvent struct {
	Action string `json:"Action"`
	Test   string `json:"Test"`
}

func main() {
	if len(os.Args) != 2 || (os.Args[1] != "recovery" && os.Args[1] != "conformance") {
		fatal(errors.New("usage: verification-runner recovery|conformance"))
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
	packages, expected := inventory(entries)
	discoveryArgs := append([]string{"test"}, packages...)
	discoveryArgs = append(discoveryArgs, "-run", "^$", "-list", "^"+prefix)
	discovery, err := runGo(discoveryArgs...)
	if err != nil {
		return err
	}
	discovered := matchingLines(discovery, prefix)
	if strings.Join(discovered, "\n") != strings.Join(expected, "\n") {
		return fmt.Errorf("%s inventory drift: expected %v; discovered %v", mode, expected, discovered)
	}
	pattern := "^(" + strings.Join(expected, "|") + ")$"
	runArgs := append([]string{"test"}, packages...)
	runArgs = append(runArgs, "-json", "-count=1", "-run", pattern)
	output, runErr := runGo(runArgs...)
	fmt.Print(output)
	if runErr != nil {
		return runErr
	}
	return verifyEvents(mode, output, expected)
}

func verifyEvents(mode, output string, expected []string) error {
	passed := map[string]bool{}
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		var event testEvent
		if json.Unmarshal(scanner.Bytes(), &event) != nil || event.Test == "" {
			continue
		}
		if event.Action == "skip" {
			return fmt.Errorf("%s test skipped: %s", mode, event.Test)
		}
		if event.Action == "pass" {
			passed[event.Test] = true
		}
	}
	for _, name := range expected {
		if !passed[name] {
			return fmt.Errorf("%s test did not pass: %s", mode, name)
		}
	}
	return scanner.Err()
}

func inventory(entries []testEntry) ([]string, []string) {
	packageSet := map[string]bool{}
	expected := make([]string, 0, len(entries))
	for _, entry := range entries {
		packageSet[entry.Package] = true
		expected = append(expected, entry.Name)
	}
	packages := make([]string, 0, len(packageSet))
	for packageName := range packageSet {
		packages = append(packages, packageName)
	}
	sort.Strings(packages)
	sort.Strings(expected)
	return packages, expected
}

func matchingLines(output, prefix string) []string {
	var result []string
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, prefix) {
			result = append(result, line)
		}
	}
	sort.Strings(result)
	return result
}

func runGo(arguments ...string) (string, error) {
	command := exec.Command("go", arguments...)
	command.Env = os.Environ()
	output, err := command.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("go %s: %w\n%s", strings.Join(arguments, " "), err, output)
	}
	return string(output), nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
