package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// A dependency's location alone cannot exempt application source. Use the
// same strict source/checksum verifier as the reviewed license inventory.
func localDependencyVerifier(backendRoot string) func(string, string) (bool, error) {
	var module, directory string
	var checked bool
	var verificationErr error
	return func(importPath, packageDirectory string) (bool, error) {
		if !checked {
			checked = true
			module, directory, verificationErr = verifiedDependencyIdentity(backendRoot)
		}
		if verificationErr != nil {
			return false, verificationErr
		}
		if importPath != module && !strings.HasPrefix(importPath, module+"/") {
			return false, nil
		}
		suffix := strings.TrimPrefix(strings.TrimPrefix(importPath, module), "/")
		if suffix != "" && (filepath.ToSlash(filepath.Clean(suffix)) != suffix || suffix == ".." || strings.HasPrefix(suffix, "../")) {
			return false, nil
		}
		expected, err := filepath.EvalSymlinks(filepath.Join(directory, filepath.FromSlash(suffix)))
		if err != nil {
			return false, err
		}
		actual, err := filepath.EvalSymlinks(packageDirectory)
		return expected == actual && err == nil, err
	}
}

func verifiedDependencyIdentity(backendRoot string) (string, string, error) {
	root := filepath.Dir(backendRoot)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "node", filepath.Join(root, "scripts", "dependencies", "verified-driver-identity.js"))
	output, err := command.Output()
	if err != nil {
		return "", "", fmt.Errorf("verify maintained dependency source: %w", err)
	}
	var proof struct {
		Module string
		Path   string
	}
	if err := json.Unmarshal(output, &proof); err != nil || proof.Module == "" || proof.Path == "" {
		return "", "", fmt.Errorf("invalid verified dependency identity")
	}
	return proof.Module, filepath.Join(backendRoot, filepath.FromSlash(proof.Path)), nil
}
