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
