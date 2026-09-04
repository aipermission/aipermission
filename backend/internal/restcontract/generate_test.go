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

func TestNormalizeRoutesRejectsDuplicatesAcrossCatalogs(t *testing.T) {
	routes := []Route{
		{Method: "get", Path: "/api/items"},
		{Method: "GET", Path: "/api/items"},
	}
	if _, err := NormalizeRoutes(routes); err == nil || !strings.Contains(err.Error(), "duplicate route GET /api/items") {
		t.Fatalf("expected combined catalog duplicate error, got %v", err)
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

func TestRouteGenerationRejectsMalformedAndAmbiguousInputs(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{name: "invalid Go", source: `package api func`, want: "parse routes source"},
		{name: "missing pattern", source: `package api; func routes() { mux.HandleFunc() }`, want: "has no pattern"},
		{name: "invalid pattern", source: `package api; func routes() { mux.HandleFunc("health", handler) }`, want: "invalid route pattern"},
		{name: "no routes", source: `package api; func routes() {}`, want: "no routes found"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ParseRoutes([]byte(test.source)); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q error, got %v", test.want, err)
			}
		})
	}

	if _, err := NormalizeRoutes([]Route{{Method: "", Path: "health"}}); err == nil || !strings.Contains(err.Error(), "invalid route") {
		t.Fatalf("expected invalid normalized route error, got %v", err)
	}
	if _, err := GenerateRoutes([]Route{
		{Method: "GET", Path: "/api/foo-bar"},
		{Method: "GET", Path: "/api/foo_bar"},
	}); err == nil || !strings.Contains(err.Error(), "operation id") {
		t.Fatalf("expected operation id collision, got %v", err)
	}
}

func TestRouteMetadataHelpersCoverSupportedShapes(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{path: "/", want: "root"},
		{path: "/api/history", want: "history"},
		{path: "/health", want: "health"},
	}
	for _, test := range tests {
		if got := routeTag(test.path); got != test.want {
			t.Errorf("routeTag(%q) = %q, want %q", test.path, got, test.want)
		}
	}
	if got := upperCamel("MIXED_value-name"); got != "MixedValueName" {
		t.Fatalf("upperCamel() = %q, want MixedValueName", got)
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
		walkSchemaRefs(t, route, contract.RequestSchema, schemas)
		for _, schema := range contract.AdditionalResponses {
			walkSchemaRefs(t, route, schema, schemas)
		}
	}
}

func TestGenerateTypesLocalConnectorActionRequestAndUncertainOutcome(t *testing.T) {
	source := []byte(`package api
func routes() { mux.HandleFunc("POST /api/connector-actions/local-run", run) }`)
	output, err := Generate(source)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(output, &document); err != nil {
		t.Fatal(err)
	}
	operation := document["paths"].(map[string]any)["/api/connector-actions/local-run"].(map[string]any)["post"].(map[string]any)
	if operation["x-aipermission-contract-level"] != "typed-request-response" || operation["requestBody"] == nil {
		t.Fatalf("local connector action contract = %#v", operation)
	}
	responses := operation["responses"].(map[string]any)
	for _, status := range []string{"200", "409", "503", "default"} {
		if responses[status] == nil {
			t.Fatalf("local connector action response %s missing: %#v", status, responses)
		}
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

func TestValidateTypedResponseRejectsShapeAndStatusDrift(t *testing.T) {
	valid := []byte(`{"items":[]}`)
	if err := ValidateTypedResponse("GET", "/api/targets", 200, valid); err != nil {
		t.Fatalf("valid target response: %v", err)
	}
	if err := ValidateTypedResponse("GET", "/api/targets", 200, []byte(`[]`)); err == nil {
		t.Fatal("bare target array should violate the response envelope")
	}
	if err := ValidateTypedResponse("GET", "/api/targets", 201, valid); err == nil {
		t.Fatal("wrong response status should violate the contract")
	}
}

func TestValidateTypedResponseRejectsUndocumentedAndInvalidFields(t *testing.T) {
	invalidStatus := []byte(`{"items":[{"id":1,"source_ref_type":"test","source_ref_id":1,"connector_kind":"ssh","activity_type":"command","target_name":"test","source":"mcp","status":"invented","action_name":"exec","title":"test","summary":"test","progress_current":0,"progress_total":0,"bytes_done":0,"bytes_total":0,"approval_required":false,"created_at":"2026-08-12T10:00:00Z","updated_at":"2026-08-12T10:00:00Z","labels":[]}],"total":1,"limit":50,"offset":0}`)
	if err := ValidateTypedResponse("GET", "/api/history", 200, invalidStatus); err == nil || !strings.Contains(err.Error(), "outside enum") {
		t.Fatalf("invalid history status error = %v", err)
	}
	extraField := []byte(`{"items":[],"total":0,"limit":50,"offset":0,"surprise":true}`)
	if err := ValidateTypedResponse("GET", "/api/history", 200, extraField); err == nil || !strings.Contains(err.Error(), "undocumented property") {
		t.Fatalf("undocumented field error = %v", err)
	}
}

func TestValidateSchemaValueRejectsEachSupportedShapeDrift(t *testing.T) {
	schemas := map[string]any{
		"Known": objectSchema(map[string]any{"name": stringSchema()}, []string{"name"}),
	}
	tests := []struct {
		name   string
		value  any
		schema map[string]any
		want   string
	}{
		{name: "unknown reference", value: map[string]any{}, schema: refSchema("Missing"), want: "unknown schema"},
		{name: "enum type", value: true, schema: enumSchema("ready"), want: "outside enum"},
		{name: "object type", value: []any{}, schema: objectSchema(nil, nil), want: "must be an object"},
		{name: "extra property", value: map[string]any{"extra": true}, schema: objectSchema(map[string]any{}, nil), want: "undocumented property"},
		{name: "required property", value: map[string]any{}, schema: objectSchema(map[string]any{"name": stringSchema()}, []string{"name"}), want: "missing required property"},
		{name: "array type", value: "wrong", schema: arraySchema(stringSchema()), want: "must be an array"},
		{name: "array item", value: []any{true}, schema: arraySchema(stringSchema()), want: "must be a string"},
		{name: "date time", value: "yesterday", schema: dateTimeSchema(), want: "RFC3339"},
		{name: "integer type", value: "1", schema: integerSchema(), want: "must be an integer"},
		{name: "integer value", value: json.Number("1.5"), schema: integerSchema(), want: "must be an integer"},
		{name: "boolean type", value: "true", schema: boolSchema(), want: "must be a boolean"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateSchemaValue("$", test.value, test.schema, schemas); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q error, got %v", test.want, err)
			}
		})
	}

	if err := validateSchemaValue("$", map[string]any{"name": "valid"}, refSchema("Known"), schemas); err != nil {
		t.Fatalf("valid referenced schema: %v", err)
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
