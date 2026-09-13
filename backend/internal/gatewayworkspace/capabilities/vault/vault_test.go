package vault

import (
	"context"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/runtimecontrol"
	secretvault "github.com/aipermission/aipermission/backend/internal/vault"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

func TestConstructorsPreserveVaultCapabilities(t *testing.T) {
	secretVault, err := secretvault.New("VaultCapabilityPassword123")
	if err != nil {
		t.Fatal(err)
	}
	sessions := &console.Manager{}
	leases := vaultsessions.NewStore()
	delivery := &vaultsessions.DeliveryCoordinator{}
	control := &runtimecontrol.State{}
	runtime := NewRuntime(nil, secretVault, nil, sessions, leases, delivery, control, nil)
	if runtime.Vault != secretVault || runtime.Sessions != sessions || runtime.Leases != leases {
		t.Fatal("Vault runtime capability did not preserve its dependencies")
	}
	configured := false
	session := NewSession(nil, sessions, leases, delivery, func(got *vaultsessions.Store, _ func(context.Context, func() error, func() error) error) {
		configured = got == leases
	}, nil)
	session.InstallAuthorizer(func(context.Context, func() error, func() error) error { return nil })
	if !configured {
		t.Fatal("session authorizer did not retain the lease store")
	}
	_ = NewMCP(nil, secretVault, control)
	_ = NewApproval(control)
}
