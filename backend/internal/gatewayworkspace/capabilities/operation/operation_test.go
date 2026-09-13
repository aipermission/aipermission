package operation

import (
	"testing"

	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/vault"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

func TestConstructorsPreserveOperationCapabilities(t *testing.T) {
	secretVault, err := vault.New("OperationCapabilityPassword123")
	if err != nil {
		t.Fatal(err)
	}
	sessions := &console.Manager{}
	delivery := &vaultsessions.DeliveryCoordinator{}
	if got := NewCommand(nil, secretVault, sessions); got.Vault != secretVault || got.Sessions != sessions {
		t.Fatal("command capability did not preserve its dependencies")
	}
	if got := NewBackup(nil, secretVault); got.Vault != secretVault {
		t.Fatal("backup capability did not preserve its Vault dependency")
	}
	_ = NewCommandBulk(sessions)
	_ = NewLiveConsole(sessions)
	_ = NewPasswordValidation(nil)
	_ = NewTransfer(nil)
	_ = NewPeerTrust(delivery)
}
