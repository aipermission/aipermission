package restcontract

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseRoutesSortsAndRejectsDuplicates(t *testing.T) {
	source := []byte(`package api
func Register() {
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
func Register() {
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
func Register() { mux.HandleFunc(pattern, handler) }`,
			want: "must be a string literal",
		},
		{
			name: "Handle registration",
			source: `package api
func Register() { mux.Handle("GET /health", handler) }`,
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

func TestParseRoutesFollowsOnlyReachableDirectRegistrationFunctions(t *testing.T) {
	source := []byte(`package api
func Register() {
	mux.HandleFunc("GET /health", health)
	registerItems(mux)
}
func registerItems(mux any) { mux.HandleFunc("GET /api/items", items) }
func dead() { fake.HandleFunc("DELETE /api/not-runtime", remove) }
`)
	routes, err := ParseRoutes(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 2 {
		t.Fatalf("reachable routes = %+v, want two", routes)
	}
	for _, route := range routes {
		if route.Path == "/api/not-runtime" {
			t.Fatal("unreachable route leaked into the generated contract")
		}
	}

	conditional := []byte(`package api
func Register() {
	if false { mux.HandleFunc("GET /api/not-runtime", handler) }
}`)
	if _, err := ParseRoutes(conditional); err == nil || !strings.Contains(err.Error(), "must be direct statements") {
		t.Fatalf("expected conditional registration rejection, got %v", err)
	}

	wrapper := []byte(`package api
func Register() { routes.Add("GET /api/not-visible", handler) }`)
	if _, err := ParseRoutes(wrapper); err == nil || !strings.Contains(err.Error(), "unsupported method call Add") {
		t.Fatalf("expected opaque wrapper rejection, got %v", err)
	}
}

func TestRouteGenerationRejectsMalformedAndAmbiguousInputs(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{name: "invalid Go", source: `package api func`, want: "parse routes source"},
		{name: "missing pattern", source: `package api; func Register() { mux.HandleFunc() }`, want: "has no pattern"},
		{name: "invalid pattern", source: `package api; func Register() { mux.HandleFunc("health", handler) }`, want: "invalid route pattern"},
		{name: "no routes", source: `package api; func Register() {}`, want: "no routes found"},
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
func Register() { mux.HandleFunc("GET /api/items/{item_id}", get) }`)
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
func Register() {
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
func Register() { mux.HandleFunc("POST /api/connector-actions/local-run", run) }`)
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

func TestGenerateTypesApprovalRunUncertainOutcome(t *testing.T) {
	source := []byte(`package api
func Register() { mux.HandleFunc("POST /api/connector-action-approvals/{id}/run", run) }`)
	output, err := Generate(source)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(output, &document); err != nil {
		t.Fatal(err)
	}
	operation := document["paths"].(map[string]any)["/api/connector-action-approvals/{id}/run"].(map[string]any)["post"].(map[string]any)
	if operation["x-aipermission-contract-level"] != "typed-request-response" || operation["requestBody"] == nil {
		t.Fatalf("approval run request contract = %#v", operation)
	}
	responses := operation["responses"].(map[string]any)
	for _, status := range []string{"200", "409", "503", "default"} {
		if responses[status] == nil {
			t.Fatalf("approval run response %s missing: %#v", status, responses)
		}
	}
}

func TestGenerateTypesSecuritySettingsAndVaultApprovalMutations(t *testing.T) {
	source := []byte(`package api
func Register() {
	mux.HandleFunc("GET /api/settings/security", getSettings)
	mux.HandleFunc("PUT /api/settings/security", updateSettings)
	mux.HandleFunc("GET /api/vault-action-approvals", listVaultApprovals)
	mux.HandleFunc("POST /api/vault-action-approvals/{id}/run", runVaultApproval)
	mux.HandleFunc("POST /api/vault-action-approvals/{id}/decline", declineVaultApproval)
}`)
	output, err := Generate(source)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(output, &document); err != nil {
		t.Fatal(err)
	}
	paths := document["paths"].(map[string]any)
	for _, item := range []struct {
		path   string
		method string
		errors []string
	}{
		{path: "/api/settings/security", method: "put", errors: []string{"400", "409"}},
		{path: "/api/vault-action-approvals/{id}/run", method: "post", errors: []string{"400", "404", "409"}},
		{path: "/api/vault-action-approvals/{id}/decline", method: "post", errors: []string{"400", "404", "409"}},
	} {
		operation := paths[item.path].(map[string]any)[item.method].(map[string]any)
		if operation["x-aipermission-contract-level"] != "typed-request-response" || operation["requestBody"] == nil {
			t.Fatalf("%s %s contract = %#v", item.method, item.path, operation)
		}
		responses := operation["responses"].(map[string]any)
		for _, status := range append([]string{"200", "default"}, item.errors...) {
			if responses[status] == nil {
				t.Fatalf("%s %s response %s missing: %#v", item.method, item.path, status, responses)
			}
		}
	}

	schemas := document["components"].(map[string]any)["schemas"].(map[string]any)
	assertAlternativeRequiredSchemaField(t, schemas, "SecuritySettingsUpdate", "expected_revision", "revision")
	assertRequiredSchemaField(t, schemas, "SecuritySettingsDocument", "revision")
	assertRequiredSchemaField(t, schemas, "ApprovalDecisionRequest", "approval_context_hash")
	assertWorkspaceHeaderParameter(t, paths["/api/settings/security"].(map[string]any)["put"].(map[string]any), true)
	approvalHash := schemas["ApprovalDecisionRequest"].(map[string]any)["properties"].(map[string]any)["approval_context_hash"].(map[string]any)
	if approvalHash["minLength"] != float64(1) || approvalHash["pattern"] != `\S` {
		t.Fatalf("approval context hash constraints = %#v", approvalHash)
	}
	vaultContext := schemas["VaultActionRequest"].(map[string]any)["properties"].(map[string]any)["approval_context"].(map[string]any)
	if vaultContext["$ref"] != "#/components/schemas/VaultApprovalContext" {
		t.Fatalf("Vault approval context schema = %#v", vaultContext)
	}
	assertPendingApprovalRequiresContextHash(t, schemas, "ConnectorActionApprovalSummary")
	assertPendingApprovalRequiresContextHash(t, schemas, "ConnectorActionApprovalDetail")
}

func assertPendingApprovalRequiresContextHash(t *testing.T, schemas map[string]any, name string) {
	t.Helper()
	schema := schemas[name].(map[string]any)
	allOf, ok := schema["allOf"].([]any)
	if !ok || len(allOf) != 1 {
		t.Fatalf("%s pending constraint = %#v", name, schema["allOf"])
	}
	condition := allOf[0].(map[string]any)
	then := condition["then"].(map[string]any)
	required := then["required"].([]any)
	hash := then["properties"].(map[string]any)["approval_context_hash"].(map[string]any)
	if len(required) != 1 || required[0] != "approval_context_hash" || hash["minLength"] != float64(1) || hash["pattern"] != `\S` {
		t.Fatalf("%s pending hash constraint = %#v", name, condition)
	}
}

func TestGenerateDocumentsConditionalWorkspaceHeaderForLockedRecoveryMutation(t *testing.T) {
	output, err := Generate([]byte(`package api
func Register() { mux.HandleFunc("POST /api/backup/import", restore) }`))
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(output, &document); err != nil {
		t.Fatal(err)
	}
	operation := document["paths"].(map[string]any)["/api/backup/import"].(map[string]any)["post"].(map[string]any)
	assertWorkspaceHeaderParameter(t, operation, false)
}

func assertWorkspaceHeaderParameter(t *testing.T, operation map[string]any, required bool) {
	t.Helper()
	parameters, _ := operation["parameters"].([]any)
	for _, raw := range parameters {
		parameter, _ := raw.(map[string]any)
		if parameter["name"] == "X-AIPermission-Workspace" && parameter["in"] == "header" {
			if parameter["required"] != required {
				t.Fatalf("workspace header required = %#v, want %t", parameter["required"], required)
			}
			return
		}
	}
	t.Fatalf("workspace header parameter missing: %#v", parameters)
}

func assertAlternativeRequiredSchemaField(t *testing.T, schemas map[string]any, schemaName string, fields ...string) {
	t.Helper()
	schema := schemas[schemaName].(map[string]any)
	alternatives, ok := schema["anyOf"].([]any)
	if !ok {
		t.Fatalf("schema %s does not define revision alternatives", schemaName)
	}
	found := map[string]bool{}
	for _, alternative := range alternatives {
		entry, ok := alternative.(map[string]any)
		if !ok {
			continue
		}
		for _, value := range entry["required"].([]any) {
			if field, ok := value.(string); ok {
				found[field] = true
			}
		}
	}
	for _, field := range fields {
		if !found[field] {
			t.Fatalf("schema %s does not accept required alternative %s: %#v", schemaName, field, alternatives)
		}
	}
}

func assertRequiredSchemaField(t *testing.T, schemas map[string]any, schemaName, field string) {
	t.Helper()
	schema := schemas[schemaName].(map[string]any)
	required := schema["required"].([]any)
	for _, value := range required {
		if value == field {
			return
		}
	}
	t.Fatalf("schema %s does not require %s: %#v", schemaName, field, required)
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
	if err := ValidateTypedResponse("GET", "/api/targets", 200, []byte(`{"items":[]} {"items":[]}`)); err == nil {
		t.Fatal("multiple JSON values should violate the contract")
	}
}

func TestValidateTypedResponseAcceptsDocumentedApprovalFailures(t *testing.T) {
	conflict := []byte(`{"error":"connector action request is no longer pending"}`)
	if err := ValidateTypedResponse("POST", "/api/connector-action-approvals/{id}/run", 409, conflict); err != nil {
		t.Fatalf("valid approval conflict: %v", err)
	}
	unknown := []byte(`{"status":"outcome_unknown","code":"connector_action_persistence_unknown","request_id":42,"error":"state unknown","assistant_hint":"inspect first"}`)
	if err := ValidateTypedResponse("POST", "/api/connector-action-approvals/{id}/run", 503, unknown); err != nil {
		t.Fatalf("valid approval uncertain outcome: %v", err)
	}
	invalid := []byte(`{"status":"failed","code":"connector_action_persistence_unknown","request_id":42,"error":"state unknown","assistant_hint":"inspect first"}`)
	if err := ValidateTypedResponse("POST", "/api/connector-action-approvals/{id}/run", 503, invalid); err == nil {
		t.Fatal("invalid approval uncertain outcome should violate the contract")
	}
	for _, path := range []string{"/api/connector-action-approvals/{id}/run", "/api/connector-action-approvals/{id}/decline"} {
		for _, status := range []int{400, 404, 409} {
			if err := ValidateTypedResponse("POST", path, status, conflict); err != nil {
				t.Fatalf("%s status %d rejected: %v", path, status, err)
			}
		}
	}
}

func TestValidateTypedResponseEnforcesPendingApprovalContextHash(t *testing.T) {
	const fields = `"id":1,"target_id":2,"target_name":"db","target_ref":"postgres:2:3","profile_id":3,"profile_label":"main","connector_kind":"postgres","action_name":"query_readonly","retry_policy":{"class":"read_only","guidance":"safe"},"created_at":"2026-09-21T01:00:00Z"`
	for _, body := range []string{
		`[{` + fields + `,"status":"approval_pending"}]`,
		`[{` + fields + `,"status":"approval_pending","approval_context_hash":"   "}]`,
	} {
		if err := ValidateTypedResponse("GET", "/api/connector-action-approvals", 200, []byte(body)); err == nil {
			t.Fatalf("invalid pending approval passed: %s", body)
		}
	}
	for _, body := range []string{
		`[{` + fields + `,"status":"approval_pending","approval_context_hash":"hash"}]`,
		`[{` + fields + `,"status":"completed"}]`,
	} {
		if err := ValidateTypedResponse("GET", "/api/connector-action-approvals", 200, []byte(body)); err != nil {
			t.Fatalf("valid approval response failed: %v", err)
		}
	}
}

func TestValidateTypedResponseRejectsUndocumentedAndInvalidFields(t *testing.T) {
	valid := []byte(`{"items":[],"limit":50,"has_more":false}`)
	if err := ValidateTypedResponse("GET", "/api/history", 200, valid); err != nil {
		t.Fatalf("valid cursor response without optional total: %v", err)
	}
	invalidStatus := []byte(`{"items":[{"id":1,"source_ref_type":"test","source_ref_id":1,"connector_kind":"ssh","activity_type":"command","target_name":"test","source":"mcp","status":"invented","action_name":"exec","title":"test","summary":"test","progress_current":0,"progress_total":0,"bytes_done":0,"bytes_total":0,"approval_required":false,"created_at":"2026-08-12T10:00:00Z","updated_at":"2026-08-12T10:00:00Z","labels":[]}],"total":1,"limit":50,"has_more":false}`)
	if err := ValidateTypedResponse("GET", "/api/history", 200, invalidStatus); err == nil || !strings.Contains(err.Error(), "outside enum") {
		t.Fatalf("invalid history status error = %v", err)
	}
	extraField := []byte(`{"items":[],"total":0,"limit":50,"has_more":false,"surprise":true}`)
	if err := ValidateTypedResponse("GET", "/api/history", 200, extraField); err == nil || !strings.Contains(err.Error(), `undocumented property "surprise"`) {
		t.Fatalf("undocumented field error = %v", err)
	}
	oldOffset := []byte(`{"items":[],"limit":50,"has_more":false,"offset":0}`)
	if err := ValidateTypedResponse("GET", "/api/history", 200, oldOffset); err == nil || !strings.Contains(err.Error(), `undocumented property "offset"`) {
		t.Fatalf("old offset response error = %v", err)
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
		{name: "nonblank minimum", value: "", schema: nonBlankStringSchema(), want: "at least 1"},
		{name: "nonblank pattern", value: "   ", schema: nonBlankStringSchema(), want: "must match pattern"},
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

func TestValidateSchemaValueEnforcesConditionalAndAlternativeBranches(t *testing.T) {
	pending := objectSchema(map[string]any{
		"status":                enumSchema("approval_pending", "completed"),
		"approval_context_hash": stringSchema(),
	}, []string{"status"})
	pending["allOf"] = []any{map[string]any{
		"if": map[string]any{
			"properties": map[string]any{"status": map[string]any{"const": "approval_pending"}},
			"required":   []string{"status"},
		},
		"then": map[string]any{
			"properties": map[string]any{"approval_context_hash": nonBlankStringSchema()},
			"required":   []string{"approval_context_hash"},
		},
	}}
	for _, value := range []map[string]any{
		{"status": "approval_pending", "approval_context_hash": "hash"},
		{"status": "completed"},
	} {
		if err := validateSchemaValue("$", value, pending, nil); err != nil {
			t.Fatalf("valid conditional value %#v: %v", value, err)
		}
	}
	for _, value := range []map[string]any{
		{"status": "approval_pending"},
		{"status": "approval_pending", "approval_context_hash": "   "},
	} {
		if err := validateSchemaValue("$", value, pending, nil); err == nil {
			t.Fatalf("invalid pending value passed: %#v", value)
		}
	}

	alternative := map[string]any{"anyOf": []any{
		map[string]any{"required": []string{"expected_revision"}},
		map[string]any{"required": []string{"revision"}},
	}}
	if err := validateSchemaValue("$", map[string]any{"revision": "r1"}, alternative, nil); err != nil {
		t.Fatalf("valid alternative: %v", err)
	}
	if err := validateSchemaValue("$", map[string]any{}, alternative, nil); err == nil {
		t.Fatal("missing alternative requirement passed")
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
