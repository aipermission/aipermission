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

type Port interface {
	DatabaseHandle() *sql.DB
	SecretVault() *vault.Vault
	TokenStore() *tokens.Store
	DatabaseOwnership() *db.DatabaseOwnership
	ClearDatabaseOwnership()
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

func (s *State) DatabaseHandle() *sql.DB {
	if s == nil {
		return nil
	}
	return s.Database
}

func (s *State) SecretVault() *vault.Vault {
	if s == nil {
		return nil
	}
	return s.Vault
}

func (s *State) TokenStore() *tokens.Store {
	if s == nil {
		return nil
	}
	return s.Tokens
}

func (s *State) DatabaseOwnership() *db.DatabaseOwnership {
	if s == nil {
		return nil
	}
	return s.Ownership
}

func (s *State) ClearDatabaseOwnership() {
	if s != nil {
		s.Ownership = nil
	}
}
