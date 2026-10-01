package architecture

import (
	"strings"
	"testing"
)

const credentialResourceOwner = modulePath + "/internal/gatewayconnectorapi/credentialresource"

func TestCredentialResourceContractsRemainPure(t *testing.T) {
	for dependency := range packageDependencies(t, credentialResourceOwner) {
		if strings.Contains(dependency, ".") || strings.HasPrefix(dependency, modulePath+"/") {
			t.Errorf("credential resource contracts must use only the standard library, imported %s", dependency)
		}
	}
}

func TestPostgresCoreDoesNotImportGatewayAdapterContracts(t *testing.T) {
	pkg := modulePath + "/internal/connectors/postgres"
	if packageDependencies(t, pkg)[modulePath+"/internal/gatewayconnectorapi"] {
		t.Errorf("%s must consume pure resource contracts without the gateway adapter API", pkg)
	}
}
