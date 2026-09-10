package connectormanagement

import (
	"context"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func TestRuntimeCredentialPreparationCopiesPublicFieldsWithoutCanonicalizer(t *testing.T) {
	input := map[string]any{"host": "database.internal"}
	ports := RuntimeCredentialPreparation(nil, nil)
	result, err := ports.Canonicalize(t.Context(), "fixture", "password", input)
	if err != nil {
		t.Fatal(err)
	}
	result["host"] = "changed"
	if input["host"] != "database.internal" {
		t.Fatal("canonicalization fallback aliased caller input")
	}
}

func TestRuntimeCredentialPortsBuildsScopedSecretAccessor(t *testing.T) {
	ports := RuntimeCredentialPorts(nil, nil, nil, nil)
	boundary := actionresult.NewCredentialBoundary(nil)
	runtimeContext := ports.RuntimeContext(
		connectortargets.Target{ID: 4, ConnectorKind: "fixture", Name: "target", Config: map[string]any{"mode": "direct"}},
		connectortargets.CredentialProfile{ID: 7, TargetID: 4, Label: "main"},
		map[string]any{"password": "secret-value"}, boundary,
	)
	value, err := runtimeContext.Secrets.GetSecret(context.Background(), "password")
	if err != nil || value != "secret-value" {
		t.Fatalf("secret value=%q error=%v", value, err)
	}
	if runtimeContext.Target.Ref != connectors.FormatTargetRef("fixture", 4, 7) {
		t.Fatalf("target ref=%q", runtimeContext.Target.Ref)
	}
	if boundary.Redact("prefix secret-value suffix") == "prefix secret-value suffix" {
		t.Fatal("credential boundary did not register accessed secret")
	}
}
