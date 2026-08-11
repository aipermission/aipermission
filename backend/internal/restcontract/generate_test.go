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

func TestGenerateTypesSharedConnectorResponses(t *testing.T) {
	source := []byte(`package api
func routes() {
	mux.HandleFunc("GET /api/targets", listTargets)
	mux.HandleFunc("GET /api/history", listHistory)
}`)
	output, err := Generate(source)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(output, &document); err != nil {
		t.Fatal(err)
	}
	components := document["components"].(map[string]any)
	schemas := components["schemas"].(map[string]any)
	for _, name := range []string{"Error", "TargetProfile", "ConnectorActionDefinition", "HistoryEntry", "AuditEntry"} {
		if schemas[name] == nil {
			t.Fatalf("missing shared schema %s", name)
		}
	}
	paths := document["paths"].(map[string]any)
	operation := paths["/api/history"].(map[string]any)["get"].(map[string]any)
	if operation["x-aipermission-contract-level"] != "typed-response" {
		t.Fatalf("history response is not typed: %+v", operation)
	}
	responses := operation["responses"].(map[string]any)
	if responses["200"] == nil || responses["default"] == nil {
		t.Fatalf("typed operation must expose success and error responses: %+v", responses)
	}
}

func TestTypedContractsReferenceDefinedSchemas(t *testing.T) {
	schemas := sharedSchemas()
	for route, contract := range typedOperationContracts() {
		walkSchemaRefs(t, route, contract.ResponseSchema, schemas)
	}
}

func TestValidateTypedRoutesRejectsRemovedRoutes(t *testing.T) {
	if err := ValidateTypedRoutes([]Route{{Method: "GET", Path: "/health"}}); err == nil || !strings.Contains(err.Error(), "unregistered route") {
		t.Fatalf("expected stale typed route error, got %v", err)
	}
}

func TestSharedSchemaRequiredFieldsExist(t *testing.T) {
	for name, raw := range sharedSchemas() {
		schema := raw.(map[string]any)
		properties, _ := schema["properties"].(map[string]any)
		required, _ := schema["required"].([]string)
		for _, field := range required {
			if properties[field] == nil {
				t.Fatalf("schema %s requires undefined property %s", name, field)
			}
		}
	}
}

func walkSchemaRefs(t *testing.T, route Route, value any, schemas map[string]any) {
	t.Helper()
	switch typed := value.(type) {
	case map[string]any:
		if ref, ok := typed["$ref"].(string); ok {
			name := strings.TrimPrefix(ref, "#/components/schemas/")
			if name == ref || schemas[name] == nil {
				t.Fatalf("%s %s references undefined schema %q", route.Method, route.Path, ref)
			}
		}
		for _, child := range typed {
			walkSchemaRefs(t, route, child, schemas)
		}
	case []any:
		for _, child := range typed {
			walkSchemaRefs(t, route, child, schemas)
		}
	case []string:
		return
	}
}
