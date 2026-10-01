package connectorcapabilities

import (
	"context"
	"net"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

type evidenceProvider struct{ value connectors.RuntimeCapability }

func (provider evidenceProvider) EvidenceCapabilities(connectorapi.EvidenceResourceRuntime) map[string]connectors.RuntimeCapability {
	return map[string]connectors.RuntimeCapability{provider.value.ConnectorRuntimeCapability(): provider.value}
}

type evidenceResources struct{}

func (evidenceResources) CredentialResources(string) connectorapi.CredentialResourceReader {
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

func TestEvidenceRejectsProtectedAuthorityUnderUnreservedNames(t *testing.T) {
	for _, value := range []connectors.RuntimeCapability{
		renamedNetwork{capability("innocent_network")},
		renamedCommand{capability("innocent_command")},
		renamedEnvironment{capability("innocent_environment")},
	} {
		t.Run(value.ConnectorRuntimeCapability(), func(t *testing.T) {
			if got, err := Evidence(evidenceProvider{value}, evidenceResources{}); err == nil || got != nil {
				t.Fatalf("renamed protected capability escaped: %#v %v", got, err)
			}
		})
	}
}
