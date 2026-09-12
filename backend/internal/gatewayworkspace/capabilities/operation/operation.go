package operation

import (
	"database/sql"

	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/vault"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

type CommandCapability struct {
	Database *sql.DB
	Vault    *vault.Vault
	Sessions *console.Manager
}

type CommandBulkCapability struct{ Sessions *console.Manager }
type LiveConsoleCapability struct{ Sessions *console.Manager }

type BackupCapability struct {
	Database *sql.DB
	Vault    *vault.Vault
}

type PasswordValidationCapability struct{ Database *sql.DB }
type TransferCapability struct{ Database *sql.DB }
type PeerTrustCapability struct {
	Delivery *vaultsessions.DeliveryCoordinator
}

func NewCommand(database *sql.DB, secretVault *vault.Vault, sessions *console.Manager) CommandCapability {
	return CommandCapability{Database: database, Vault: secretVault, Sessions: sessions}
}

func NewCommandBulk(sessions *console.Manager) CommandBulkCapability {
	return CommandBulkCapability{Sessions: sessions}
}

func NewLiveConsole(sessions *console.Manager) LiveConsoleCapability {
	return LiveConsoleCapability{Sessions: sessions}
}

func NewBackup(database *sql.DB, secretVault *vault.Vault) BackupCapability {
	return BackupCapability{Database: database, Vault: secretVault}
}

func NewPasswordValidation(database *sql.DB) PasswordValidationCapability {
	return PasswordValidationCapability{Database: database}
}

func NewTransfer(database *sql.DB) TransferCapability { return TransferCapability{Database: database} }

func NewPeerTrust(delivery *vaultsessions.DeliveryCoordinator) PeerTrustCapability {
	return PeerTrustCapability{Delivery: delivery}
}
