// Package storage defines the workspace boundary's encrypted-storage port.
package storage

import (
	"database/sql"

	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

type Ownership interface {
	Release() (bool, error)
}

type Port interface {
	DatabaseHandle() *sql.DB
	SecretVault() *vault.Vault
	TokenStore() *tokens.Store
	DatabaseOwnership() Ownership
	ClearDatabaseOwnership()
}
