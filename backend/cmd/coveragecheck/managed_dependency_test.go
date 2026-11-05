package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func managedDependencyFixture(t *testing.T) (string, string) {
	t.Helper()
	repository, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.CopyFS(filepath.Join(root, "third_party"), os.DirFS(filepath.Join(repository, "third_party"))); err != nil {
		t.Fatal(err)
	}
	var inventory struct{ Sources []struct{ Path string } }
	data, err := os.ReadFile(filepath.Join(repository, "docs/security/dependency-licenses.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &inventory); err != nil {
		t.Fatal(err)
	}
	files := []string{"docs/security/dependency-licenses.json", "scripts/dependency-license-check.js", "scripts/dependency-license-inputs.js", "scripts/dependencies/verified-driver-identity.js", "scripts/dependencies/patched-driver.js"}
	for _, source := range inventory.Sources {
		files = append(files, source.Path)
	}
	for _, file := range files {
		content, err := os.ReadFile(filepath.Join(repository, file))
		if err != nil {
			t.Fatal(err)
		}
		location := filepath.Join(root, file)
		if err := os.MkdirAll(filepath.Dir(location), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(location, content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root, filepath.Join(root, "backend")
}

func TestCoverageDependencyProofRejectsActualGoModPinDrift(t *testing.T) {
	root, backend := managedDependencyFixture(t)
	file, err := os.OpenFile(filepath.Join(backend, "go.mod"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.WriteString("\n// changed dependency pin or replacement\n")
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if writeErr != nil {
		t.Fatal(writeErr)
	}
	verified, err := localDependencyVerifier(backend)("github.com/microsoft/go-mssqldb", filepath.Join(root, "third_party", "go-mssqldb"))
	if verified || err == nil {
		t.Fatalf("unreviewed actual go.mod accepted: %v %v", verified, err)
	}
}

func TestCoverageAcceptsOnlySourceVerifiedThirdPartyPackageIdentity(t *testing.T) {
	root, backend := managedDependencyFixture(t)
	driver := filepath.Join(root, "third_party", "go-mssqldb")
	verify := localDependencyVerifier(backend)
	const module = "github.com/microsoft/go-mssqldb"
	for _, tc := range []struct {
		imported, directory string
		want                bool
	}{
		{module, driver, true},
		{module + "/aecmk", filepath.Join(driver, "aecmk"), true},
		{"example.invalid/hidden", driver, false},
		{module, backend, false},
		{module + "/../backend", backend, false},
		{module + "/aecmk", driver, false},
	} {
		got, err := verify(tc.imported, tc.directory)
		if err != nil || got != tc.want {
			t.Fatalf("dependency %s -> %s: %v %v", tc.imported, tc.directory, got, err)
		}
	}
	if err := os.WriteFile(filepath.Join(driver, "aecmk", "hidden.go"), []byte("package aecmk\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if verified, err := localDependencyVerifier(backend)(module, driver); err == nil || verified {
		t.Fatalf("modified/extra application source entered the third-party exemption: %v %v", verified, err)
	}
}

func TestProductionInventoryRejectsModifiedMaintainedDependency(t *testing.T) {
	root, backend := managedDependencyFixture(t)
	application := filepath.Join(backend, "internal", "fixture")
	if err := os.MkdirAll(application, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(application, "source.go"), []byte("package fixture\nfunc Run(){}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	input := fmt.Sprintf(`{"ImportPath":%q,"Dir":%q,"GoFiles":["source.go"]}
{"ImportPath":"github.com/microsoft/go-mssqldb/aecmk","Dir":%q,"GoFiles":["keyprovider.go"]}`, backendModulePath+"/internal/fixture", application, filepath.Join(root, "third_party", "go-mssqldb", "aecmk"))
	packages, err := readProductionPackagesAt(strings.NewReader(input), backend)
	if err != nil || len(packages) != 1 || packages["internal/fixture"] == nil {
		t.Fatalf("verified dependency lost application inventory: %#v %v", packages, err)
	}
	if err := os.WriteFile(filepath.Join(root, "third_party", "go-mssqldb", "mssql.go"), []byte("package mssql\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readProductionPackagesAt(strings.NewReader(input), backend); err == nil {
		t.Fatal("tampered driver source bypassed the application source oracle")
	}
}
