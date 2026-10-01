package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
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
	output := string(generateFrontendContract(false))
	for _, expected := range []string{`"approval_pending"`, `"outcome_unknown"`, `"non_idempotent"`, `"target_ref"`, "DO NOT EDIT"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("generated frontend contract does not contain %s", expected)
		}
	}
	typed := string(generateFrontendContract(true))
	for _, expected := range []string{`as const`, `"completed"`, `"non_idempotent"`, `"target_ref"`} {
		if !strings.Contains(typed, expected) {
			t.Fatalf("generated TypeScript contract does not contain %s", expected)
		}
	}
	if strings.Contains(output, "as const") {
		t.Fatal("MCP JavaScript contract must not contain TypeScript syntax")
	}
}

func TestCommittedFrontendContractIsCurrent(t *testing.T) {
	contractPath := filepath.Join("..", "..", "..", "frontend", "src", "lib", "gateway-contracts", "generated-connector-contract.ts")
	mcpPath := filepath.Join("..", "..", "..", "packages", "mcp", "src", "generated-connector-contract.js")
	assertGeneratedFile(t, contractPath, generateFrontendContract(true))
	assertGeneratedFile(t, mcpPath, generateFrontendContract(false))
}

func TestGeneratedActionInputLimitUsesCanonicalBudget(t *testing.T) {
	for _, typescript := range []bool{false, true} {
		output := string(generateFrontendContract(typescript))
		want := fmt.Sprintf("export const connectorActionMaximumInputBytes = %d;", connectors.MaximumActionInputBytes)
		if !strings.Contains(output, want) {
			t.Fatalf("generated budget missing: %s", output)
		}
	}
}

func TestRuntimeListUsesFrontendWidthWithoutChangingMCPFormat(t *testing.T) {
	for _, tc := range []struct {
		name       string
		typescript bool
		multiline  bool
	}{
		{name: "typescript", typescript: true},
		{name: "javascript", multiline: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output strings.Builder
			writeRuntimeList(&output, "formatBoundary", []string{strings.Repeat("x", 80)}, tc.typescript)
			if strings.Contains(output.String(), "[\n") != tc.multiline {
				t.Fatalf("unexpected runtime list layout: %s", output.String())
			}
		})
	}
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
