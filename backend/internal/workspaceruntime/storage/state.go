package storage

import (
	"database/sql"

	"github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

type State struct {
	Database  *sql.DB
	Vault     *vault.Vault
	Tokens    *tokens.Store
	Ownership *db.DatabaseOwnership
}

func New(database *sql.DB, secretVault *vault.Vault, tokenStore *tokens.Store, workspaceUUID string, ownership *db.DatabaseOwnership) State {
	if tokenStore == nil {
		tokenStore = tokens.NewEncryptedStore(database, secretVault, workspaceUUID)
	}
	return State{
		Database:  database,
		Vault:     secretVault,
		Tokens:    tokenStore,
		Ownership: ownership,
	}
}
