// Package maintenancepolicy loads the repository's machine-readable
// architecture and maintainability limits for Go-based development gates.
package maintenancepolicy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Policy struct {
	Version       int           `json:"version"`
	GoFunction    GoFunction    `json:"goFunction"`
	BackendFanout BackendFanout `json:"backendFanout"`
}

type GoFunction struct {
	ProductionMaxLines      int                       `json:"productionMaxLines"`
	ProductionMaxComplexity int                       `json:"productionMaxComplexity"`
	TestMaxLines            int                       `json:"testMaxLines"`
	TestMaxComplexity       int                       `json:"testMaxComplexity"`
	Overrides               map[string]FunctionBudget `json:"overrides"`
}

type FunctionBudget struct {
	Lines      int `json:"lines"`
	Complexity int `json:"complexity"`
}

type BackendFanout struct {
	PackageMax                 int            `json:"packageMax"`
	OwnerMax                   int            `json:"ownerMax"`
	FamilyOwnerMax             int            `json:"familyOwnerMax"`
	TestFileInternalImportsMax int            `json:"testFileInternalImportsMax"`
	Overrides                  map[string]int `json:"overrides"`
}

func Load() (Policy, error) {
	path, err := findPolicyPath()
	if err != nil {
		return Policy{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Policy{}, fmt.Errorf("read maintenance policy: %w", err)
	}
	var policy Policy
	if err := json.Unmarshal(data, &policy); err != nil {
		return Policy{}, fmt.Errorf("parse maintenance policy: %w", err)
	}
	if policy.Version != 1 || policy.GoFunction.ProductionMaxLines <= 0 || policy.BackendFanout.PackageMax <= 0 || policy.BackendFanout.FamilyOwnerMax <= 0 {
		return Policy{}, fmt.Errorf("maintenance policy is incomplete or unsupported")
	}
	return policy, nil
}

func MustLoad() Policy {
	policy, err := Load()
	if err != nil {
		panic(err)
	}
	return policy
}

func findPolicyPath() (string, error) {
	directory, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}
	for {
		candidate := filepath.Join(directory, "maintenance-policy.json")
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			return candidate, nil
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return "", fmt.Errorf("maintenance-policy.json not found from %s", directory)
		}
		directory = parent
	}
}
