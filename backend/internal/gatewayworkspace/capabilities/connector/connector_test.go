package connector

import (
	"testing"

	"github.com/aipermission/aipermission/backend/internal/runtimecontrol"
	"github.com/aipermission/aipermission/backend/internal/securitypolicy"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vault"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

func TestConstructorsPreserveConnectorCapabilities(t *testing.T) {
	tokenStore := &tokens.Store{}
	secretVault, err := vault.New("ConnectorCapabilityPassword123")
	if err != nil {
		t.Fatal(err)
	}
	policy := securitypolicy.NewService(nil)
	delivery := &vaultsessions.DeliveryCoordinator{}
	control := &runtimecontrol.State{}
	action := NewAction(nil, tokenStore, nil, secretVault, policy, delivery, control)
	if action.Tokens != tokenStore || action.Vault != secretVault || action.Control != control {
		t.Fatal("action capability did not preserve its dependencies")
	}
	transport := NewTransport(nil, nil, delivery)
	if transport.Delivery != delivery {
		t.Fatalf("transport capability = %#v", transport)
	}
	_ = NewApproval(nil, control)
	_ = NewCatalog(nil, nil)
	_ = NewCredential(secretVault)
	_ = NewManagement(nil, nil, delivery)
}
