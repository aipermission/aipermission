package gatewayinfrastructure

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace"
	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace/runtime/storage"
)

func TestWorkspaceHandlesRemainDistinctAndComponentScoped(t *testing.T) {
	component := NewComponent(t.TempDir(), nil)
	firstOwner := &gatewayworkspace.Runtime{Identity: gatewayworkspace.RuntimeIdentity{DatabaseID: "first"}}
	secondOwner := &gatewayworkspace.Runtime{Identity: gatewayworkspace.RuntimeIdentity{DatabaseID: "second"}}

	first := component.handleFor(firstOwner)
	second := component.handleFor(secondOwner)
	if first == nil || second == nil || first == second || first.token == second.token {
		t.Fatal("independent workspace owners did not receive distinct capabilities")
	}
	if owner, ok := component.resolve(first); !ok || owner != firstOwner {
		t.Fatal("first workspace capability stopped resolving after adding the second")
	}
	if owner, ok := component.resolve(second); !ok || owner != secondOwner {
		t.Fatal("second workspace capability did not resolve to its owner")
	}

	foreign := NewComponent(t.TempDir(), nil)
	if _, ok := foreign.resolve(first); ok {
		t.Fatal("workspace capability resolved in a foreign component")
	}
	component.forgetHandle(first)
	if _, ok := component.resolve(first); ok {
		t.Fatal("forgotten workspace capability remained valid")
	}
}

type metadataReaderFactory struct {
	database *sql.DB
	allowed  bool
	err      error
}

func (factory *metadataReaderFactory) ForDatabase(database *sql.DB) gatewayaccess.VaultMetadataReader {
	factory.database = database
	return factory
}

func (factory *metadataReaderFactory) CanRead(context.Context, int64, int64, time.Time) (bool, error) {
	return factory.allowed, factory.err
}

func TestVaultMetadataReadKeepsWorkspaceDatabaseInsideInfrastructure(t *testing.T) {
	database := &sql.DB{}
	storageState := storage.New(database, nil, nil, "workspace", nil)
	owner := &gatewayworkspace.Runtime{Storage: &storageState}
	component := NewComponent(t.TempDir(), nil)
	handle := component.handleFor(owner)
	factory := &metadataReaderFactory{allowed: true}

	allowed, err := component.CanReadVaultMetadata(t.Context(), handle, factory, 7, 11, time.Unix(123, 0))
	if err != nil || !allowed {
		t.Fatalf("allowed=%t err=%v", allowed, err)
	}
	if factory.database != database {
		t.Fatal("metadata reader did not receive the resolved workspace database")
	}

	foreign := NewComponent(t.TempDir(), nil)
	if allowed, err := foreign.CanReadVaultMetadata(t.Context(), handle, factory, 7, 11, time.Now()); allowed || !errors.Is(err, gatewayaccess.ErrVaultMetadataAccessUnavailable) {
		t.Fatalf("foreign handle allowed=%t err=%v", allowed, err)
	}
}
