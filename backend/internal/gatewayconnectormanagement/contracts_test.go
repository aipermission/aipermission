package gatewayconnectormanagement

import (
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func TestActionRequestBoundaryDoesNotShareMutableState(t *testing.T) {
	domain := connectortargets.ActionRequest{
		Preview: map[string]any{"nested": map[string]any{"value": "original"}},
		Input:   map[string]any{"items": []any{map[string]any{"value": "original"}}},
		Output:  map[string]any{"bytes": []byte("original")},
		RetryPolicy: connectors.RetryPolicy{
			Class: connectors.RetryConditional, PreconditionFields: []string{"version"},
		},
	}
	adopted := AdoptActionRequest(domain)
	adopted.Preview["nested"].(map[string]any)["value"] = "changed"
	adopted.Input["items"].([]any)[0].(map[string]any)["value"] = "changed"
	adopted.Output.(map[string]any)["bytes"].([]byte)[0] = 'X'
	adopted.RetryPolicy.PreconditionFields[0] = "changed"

	if domain.Preview["nested"].(map[string]any)["value"] != "original" ||
		domain.Input["items"].([]any)[0].(map[string]any)["value"] != "original" ||
		string(domain.Output.(map[string]any)["bytes"].([]byte)) != "original" ||
		domain.RetryPolicy.PreconditionFields[0] != "version" {
		t.Fatalf("adopted request retained mutable domain aliases: %#v", domain)
	}

	released := ReleaseActionRequest(adopted)
	released.Preview["nested"].(map[string]any)["value"] = "released"
	released.RetryPolicy.PreconditionFields[0] = "released"
	if adopted.Preview["nested"].(map[string]any)["value"] != "changed" ||
		adopted.RetryPolicy.PreconditionFields[0] != "changed" {
		t.Fatalf("released request retained mutable gateway aliases: %#v", adopted)
	}
}

func TestTargetAndCredentialBoundaryDoNotShareNestedMaps(t *testing.T) {
	domainTarget := connectortargets.Target{Config: map[string]any{"nested": map[string]any{"value": "target"}}}
	domainProfile := connectortargets.CredentialProfile{Public: map[string]any{"nested": []any{"profile"}}}
	target := AdoptTarget(domainTarget)
	profile := AdoptCredentialProfile(domainProfile)
	target.Config["nested"].(map[string]any)["value"] = "changed"
	profile.Public["nested"].([]any)[0] = "changed"
	if domainTarget.Config["nested"].(map[string]any)["value"] != "target" ||
		domainProfile.Public["nested"].([]any)[0] != "profile" {
		t.Fatal("gateway DTOs retained nested domain aliases")
	}
}
