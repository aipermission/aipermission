package restcontract

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseRoutesSortsAndRejectsDuplicates(t *testing.T) {
	source := []byte(`package api
func routes() {
	mux.HandleFunc("POST /api/items/{id}", post)
	mux.HandleFunc("GET /health", health)
}`)
	routes, err := ParseRoutes(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 2 || routes[0] != (Route{Method: "POST", Path: "/api/items/{id}"}) || routes[1] != (Route{Method: "GET", Path: "/health"}) {
		t.Fatalf("unexpected routes: %+v", routes)
	}

	duplicate := []byte(`package api
func routes() {
	mux.HandleFunc("GET /health", first)
	mux.HandleFunc("GET /health", second)
}`)
	if _, err := ParseRoutes(duplicate); err == nil || !strings.Contains(err.Error(), "duplicate route") {
		t.Fatalf("expected duplicate route error, got %v", err)
	}
}

func TestParseRoutesRejectsRegistrationsThatCouldEscapeTheContract(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "dynamic HandleFunc pattern",
			source: `package api
func routes() { mux.HandleFunc(pattern, handler) }`,
			want: "must be a string literal",
		},
		{
			name: "Handle registration",
			source: `package api
func routes() { mux.Handle("GET /health", handler) }`,
			want: "unsupported Handle route registration",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ParseRoutes([]byte(test.source)); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q error, got %v", test.want, err)
			}
		})
	}
}

func TestGenerateProducesBoundedRouteInventory(t *testing.T) {
	source := []byte(`package api
func routes() { mux.HandleFunc("GET /api/items/{item_id}", get) }`)
	output, err := Generate(source)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(output, &document); err != nil {
		t.Fatal(err)
	}
	paths := document["paths"].(map[string]any)
	operation := paths["/api/items/{item_id}"].(map[string]any)["get"].(map[string]any)
	if operation["operationId"] != "getApiItemsByItemId" || operation["x-aipermission-contract-level"] != "route-inventory" {
		t.Fatalf("unexpected operation: %+v", operation)
	}
	parameters := operation["parameters"].([]any)
	schema := parameters[0].(map[string]any)["schema"].(map[string]any)
	if schema["type"] != "integer" {
		t.Fatalf("unexpected path parameter: %+v", parameters[0])
	}
}
