package gatewayinfrastructure

import (
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectorcapabilities"
	"github.com/aipermission/aipermission/backend/internal/connectorruntime"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

type evidenceCapabilityProvider struct {
	provided map[string]connectors.RuntimeCapability
	seen     connectorapi.EvidenceResourceRuntime
	calls    int
}

func (provider *evidenceCapabilityProvider) EvidenceCapabilities(runtime connectorapi.EvidenceResourceRuntime) map[string]connectors.RuntimeCapability {
	provider.calls++
	provider.seen = runtime
	return provider.provided
}

type allCapabilityProviders struct {
	*scopedCapabilityProvider
	*actionCapabilityProvider
	*evidenceCapabilityProvider
}

func TestEvidenceCapabilityCompositionCannotInvokeMutableOrActionProviders(t *testing.T) {
	resource := &scopedCapabilityProvider{}
	action := &actionCapabilityProvider{}
	evidence := &evidenceCapabilityProvider{provided: map[string]connectors.RuntimeCapability{
		"local_evidence": testRuntimeCapability("local_evidence"),
	}}
	runtime := connectorruntime.EvidenceResources(connectorruntime.NewScope("fixture", connectorruntime.Dependencies{}).ScopedResourceRuntime())
	result, err := connectorcapabilities.Evidence(allCapabilityProviders{resource, action, evidence}, runtime)
	if err != nil || len(result) != 1 || result["local_evidence"] == nil || evidence.calls != 1 || evidence.seen != runtime || resource.calls != 0 || action.calls != 0 {
		t.Fatalf("evidence authority crossed boundary: %#v %v calls=%d/%d/%d", result, err, resource.calls, action.calls, evidence.calls)
	}
	delete(evidence.provided, "local_evidence")
	if result["local_evidence"] == nil {
		t.Fatal("composed evidence capabilities alias mutable provider map")
	}
}

func TestEvidenceCapabilityCompositionRejectsMissingAndProtectedAuthority(t *testing.T) {
	runtime := connectorruntime.EvidenceResources(connectorruntime.NewScope("fixture", connectorruntime.Dependencies{}).ScopedResourceRuntime())
	for _, adapter := range []connectorapi.Adapter{nil, struct{}{}, (*evidenceCapabilityProvider)(nil), &actionCapabilityProvider{}, &scopedCapabilityProvider{}} {
		if result, err := connectorcapabilities.Evidence(adapter, runtime); err == nil || result != nil {
			t.Fatalf("missing evidence provider accepted: %#v %v", result, err)
		}
	}
	provider := &evidenceCapabilityProvider{}
	if result, err := connectorcapabilities.Evidence(provider, nil); err == nil || result != nil || provider.calls != 0 {
		t.Fatal("missing resources invoked evidence provider")
	}
	for name, capability := range map[string]connectors.RuntimeCapability{
		"invalid-name": testRuntimeCapability("invalid-name"), "null_evidence": nil,
		"wrong_identity": testRuntimeCapability("other"),
		connectors.NetworkTransportCapabilityName:   testRuntimeCapability(connectors.NetworkTransportCapabilityName),
		connectors.CommandTransportCapabilityName:   testRuntimeCapability(connectors.CommandTransportCapabilityName),
		connectors.SessionEnvironmentCapabilityName: testRuntimeCapability(connectors.SessionEnvironmentCapabilityName),
	} {
		provider.provided = map[string]connectors.RuntimeCapability{name: capability}
		if result, err := connectorcapabilities.Evidence(provider, runtime); err == nil || result != nil {
			t.Fatalf("invalid/protected evidence capability %q accepted: %#v %v", name, result, err)
		}
	}
}

func TestEvidenceRuntimeApplicationRejectsUnavailableWorkspace(t *testing.T) {
	var application *ConnectorRuntimeApplication
	if result, err := application.CleanupEvidenceCapabilities(nil, "fixture"); err == nil || result != nil {
		t.Fatal("nil runtime/handle accepted")
	}
}
