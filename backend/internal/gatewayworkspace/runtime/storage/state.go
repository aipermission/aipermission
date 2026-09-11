package storage

import (
	"database/sql"

	"github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

type State struct {
	database  *sql.DB
	vault     *vault.Vault
	tokens    *tokens.Store
	ownership *db.DatabaseOwnership
}

func New(database *sql.DB, secretVault *vault.Vault, tokenStore *tokens.Store, workspaceUUID string, ownership *db.DatabaseOwnership) State {
	if tokenStore == nil {
		tokenStore = tokens.NewEncryptedStore(database, secretVault, workspaceUUID)
	}
	return State{database: database, vault: secretVault, tokens: tokenStore, ownership: ownership}
}

func (s *State) DatabaseHandle() *sql.DB {
	if s == nil {
		return nil
	}
	return s.database
}

func (s *State) SecretVault() *vault.Vault {
	if s == nil {
		return nil
	}
	return s.vault
}

func (s *State) TokenStore() *tokens.Store {
	if s == nil {
		return nil
	}
	return s.tokens
}

func (s *State) DatabaseOwnership() *db.DatabaseOwnership {
	if s == nil {
		return nil
	}
	return s.ownership
}

func (s *State) ClearDatabaseOwnership() {
	if s != nil {
		s.ownership = nil
	}
}

var _ Port = (*State)(nil)
