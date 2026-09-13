package storage

import (
	"testing"

	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

type testOwnership struct{}

func (testOwnership) Release() (bool, error) { return true, nil }

func TestStateOwnsAndClearsStorageCapabilities(t *testing.T) {
	secretVault, err := vault.New("StorageStatePassword123")
	if err != nil {
		t.Fatal(err)
	}
	tokenStore := &tokens.Store{}
	state := New(nil, secretVault, tokenStore, "workspace", testOwnership{})
	if state.DatabaseHandle() != nil || state.SecretVault() != secretVault || state.TokenStore() != tokenStore || state.DatabaseOwnership() == nil {
		t.Fatal("storage state did not preserve its dependencies")
	}
	state.ClearDatabaseOwnership()
	if state.DatabaseOwnership() != nil {
		t.Fatal("database ownership was not cleared")
	}
	var nilState *State
	if nilState.DatabaseHandle() != nil || nilState.SecretVault() != nil || nilState.TokenStore() != nil || nilState.DatabaseOwnership() != nil {
		t.Fatal("nil storage state exposed capabilities")
	}
	nilState.ClearDatabaseOwnership()
}
