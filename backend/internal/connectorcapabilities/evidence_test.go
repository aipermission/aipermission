package connectorcapabilities

import (
	"context"
	"net"
	"reflect"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	resourcecontract "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi/credentialresource"
)

type evidenceProvider struct {
	combinedProvider
	provided map[string]connectors.RuntimeCapability
	seen     resourcecontract.EvidenceResourceRuntime
	calls    int
}

func (provider *evidenceProvider) EvidenceCapabilities(resources resourcecontract.EvidenceResourceRuntime) map[string]connectors.RuntimeCapability {
	provider.calls++
	provider.seen = resources
	return provider.provided
}

func newEvidenceProvider(provided Set) *evidenceProvider {
	return &evidenceProvider{combinedProvider: combinedProvider{&scopedProvider{}, &actionProvider{}}, provided: provided}
}

func (provider *evidenceProvider) dispatch() [3]int {
	return [3]int{provider.calls, provider.scopedProvider.calls, provider.actionProvider.calls}
}

type evidenceResources struct{}

func (evidenceResources) CredentialResources(string) resourcecontract.CredentialResourceReader {
	panic("composition must not read evidence")
}

type renamedNetwork struct{ capability }

func (renamedNetwork) DialConnectorTCP(context.Context, connectors.NetworkDialRequest) (net.Conn, error) {
	panic("evidence must not dial")
}

type renamedCommand struct{ capability }

func (renamedCommand) RunConnectorCommand(context.Context, connectors.CommandRunRequest) (connectors.CommandRunResult, error) {
	panic("evidence must not execute")
}

type renamedEnvironment struct{ capability }

func (renamedEnvironment) SessionEnvironmentVersion() string { panic("evidence must not inject") }
func (renamedEnvironment) SessionEnvironmentPeerIdentityRequired() bool {
	panic("evidence must not inspect environment peers")
}

func TestEvidenceRejectsProtectedAuthorityByNameAndMethods(t *testing.T) {
	for _, value := range []connectors.RuntimeCapability{
		capability(connectors.NetworkTransportCapabilityName),
		capability(connectors.CommandTransportCapabilityName),
		capability(connectors.SessionEnvironmentCapabilityName),
		renamedNetwork{capability("innocent_network")},
		renamedCommand{capability("innocent_command")},
		renamedEnvironment{capability("innocent_environment")},
	} {
		t.Run(value.ConnectorRuntimeCapability(), func(t *testing.T) {
			provider := newEvidenceProvider(Set{value.ConnectorRuntimeCapability(): value})
			if got, err := Evidence(provider, evidenceResources{}); err == nil || !strings.Contains(err.Error(), "must not expose protected runtime capability") || got != nil || provider.dispatch() != [3]int{1, 0, 0} {
				t.Fatalf("protected capability escaped: %#v %v dispatch=%v", got, err, provider.dispatch())
			}
		})
	}
}

func TestEvidenceRejectsMissingDependenciesWithoutDispatch(t *testing.T) {
	for _, missing := range []string{"adapter", "contract", "typed nil provider", "resources", "typed nil resources"} {
		t.Run(missing, func(t *testing.T) {
			provider := newEvidenceProvider(nil)
			var adapter any = provider
			var resources resourcecontract.EvidenceResourceRuntime = evidenceResources{}
			switch missing {
			case "adapter":
				adapter = nil
			case "contract":
				adapter = provider.combinedProvider
			case "typed nil provider":
				adapter = (*evidenceProvider)(nil)
			case "resources":
				resources = nil
			case "typed nil resources":
				resources = (*evidenceResources)(nil)
			}
			got, err := Evidence(adapter, resources)
			if err == nil || err.Error() != "connector cleanup evidence runtime is unavailable" || got != nil || provider.dispatch() != [3]int{} {
				t.Fatalf("missing evidence dependency dispatched: %#v %v dispatch=%v", got, err, provider.dispatch())
			}
		})
	}
}

func TestEvidenceValidatesProviderMapBeforeGrantingAuthority(t *testing.T) {
	for _, invalid := range invalidCapabilities() {
		if invalid.name == "protected" {
			continue
		}
		t.Run(invalid.name, func(t *testing.T) {
			provider := newEvidenceProvider(Set{invalid.name: invalid.value})
			got, err := Evidence(provider, evidenceResources{})
			if err == nil || !strings.Contains(err.Error(), invalid.message) || got != nil || provider.dispatch() != [3]int{1, 0, 0} || len(provider.provided) != 1 {
				t.Fatalf("invalid evidence composition=%#v %v dispatch=%v", got, err, provider.dispatch())
			}
		})
	}
}

func TestEvidenceComposesOnlyReadOnlyProviderAndCopiesItsMap(t *testing.T) {
	for _, provided := range []Set{nil, {"cleanup_journal": capability("cleanup_journal")}} {
		provider := newEvidenceProvider(provided)
		resources := evidenceResources{}
		got, err := Evidence(provider, resources)
		want := Set{}
		if provided != nil {
			want["cleanup_journal"] = capability("cleanup_journal")
		}
		if err != nil || got == nil || !reflect.DeepEqual(got, want) || provider.dispatch() != [3]int{1, 0, 0} || provider.seen != resources {
			t.Fatalf("read-only composition=%#v %v dispatch=%v", got, err, provider.dispatch())
		}
		delete(provided, "cleanup_journal")
		if !reflect.DeepEqual(got, want) {
			t.Fatal("evidence composition aliases provider map")
		}
	}
}
