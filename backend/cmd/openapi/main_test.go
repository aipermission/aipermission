package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateContractUsesCoreAndConnectorRoutes(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "internal", "api", "httptransport", "routes.go"))
	if err != nil {
		t.Fatal(err)
	}
	output, err := generateContract(source)
	if err != nil {
		t.Fatal(err)
	}
	text := string(output)
	for _, route := range []string{`"/api/targets"`, `"/api/connectors/ssh/config/parse"`} {
		if !strings.Contains(text, route) {
			t.Fatalf("generated contract does not contain %s", route)
		}
	}
}

func TestGenerateContractRejectsInvalidRoutes(t *testing.T) {
	if _, err := generateContract([]byte("not Go source")); err == nil || !strings.Contains(err.Error(), "parse core routes") {
		t.Fatalf("invalid route error = %v", err)
	}
}

func TestGenerateFrontendContractUsesCanonicalEnums(t *testing.T) {
	output := string(generateFrontendContract())
	for _, expected := range []string{`"approval_pending"`, `"outcome_unknown"`, `"non_idempotent"`, `"target_ref"`, "DO NOT EDIT"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("generated frontend contract does not contain %s", expected)
		}
	}
	declaration := string(generateFrontendContractDeclaration())
	for _, expected := range []string{`readonly [`, `"completed"`, `"non_idempotent"`, `"target_ref"`} {
		if !strings.Contains(declaration, expected) {
			t.Fatalf("generated frontend declaration does not contain %s", expected)
		}
	}
}

func TestCommittedFrontendContractIsCurrent(t *testing.T) {
	contractPath := filepath.Join("..", "..", "..", "frontend", "src", "lib", "gateway-contracts", "generated-connector-contract.js")
	mcpPath := filepath.Join("..", "..", "..", "packages", "mcp", "src", "generated-connector-contract.js")
	declarationPath := frontendDeclarationPath(contractPath)
	assertGeneratedFile(t, contractPath, generateFrontendContract())
	assertGeneratedFile(t, mcpPath, generateFrontendContract())
	assertGeneratedFile(t, declarationPath, generateFrontendContractDeclaration())
}

func assertGeneratedFile(t *testing.T, path string, expected []byte) {
	t.Helper()
	actual, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != string(expected) {
		t.Fatalf("%s is stale; run make rest-contract", path)
	}
}
