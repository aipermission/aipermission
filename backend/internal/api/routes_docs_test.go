package api

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/restcontract"
)

func TestRESTDocsMentionRegisteredRoutes(t *testing.T) {
	routesSource, err := os.ReadFile("routes.go")
	if err != nil {
		t.Fatalf("read routes: %v", err)
	}
	docs, err := os.ReadFile("../../../docs/api/rest-api.md")
	if err != nil {
		t.Fatalf("read rest docs: %v", err)
	}
	docText := string(docs)
	routes, err := restcontract.ParseRoutes(routesSource)
	if err != nil {
		t.Fatalf("parse registered routes: %v", err)
	}
	for _, route := range routes {
		if !strings.Contains(docText, route.Path) {
			t.Fatalf("REST docs do not mention registered route %s %s", route.Method, route.Path)
		}
	}
}

func TestGeneratedOpenAPIMatchesRegisteredRoutes(t *testing.T) {
	routesSource, err := os.ReadFile("routes.go")
	if err != nil {
		t.Fatalf("read routes: %v", err)
	}
	expected, err := restcontract.Generate(routesSource)
	if err != nil {
		t.Fatalf("generate OpenAPI route inventory: %v", err)
	}
	current, err := os.ReadFile("../../../docs/api/openapi.json")
	if err != nil {
		t.Fatalf("read generated OpenAPI route inventory: %v", err)
	}
	if !bytes.Equal(current, expected) {
		t.Fatal("generated OpenAPI route inventory is stale; run make rest-contract")
	}
}
