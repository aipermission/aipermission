package foundation

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/db"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vault"
	"github.com/aipermission/aipermission/backend/internal/workspacelifecycle"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime/foundation/identity"
)

type State struct {
	ID              string
	Path            string
	Database        *sql.DB
	Ownership       *db.DatabaseOwnership
	Registry        connectors.Catalog
	AdapterRegistry connectorapi.Catalog
	TokenStore      *tokens.Store
	Identity        identity.State
}

type OpenInput struct {
	ID                      string
	Path                    string
	Password                string
	ConfiguredGatewaySecret string
	Registry                connectors.Catalog
	AdapterRegistry         connectorapi.Catalog
}

type AdoptInput struct {
	ID                      string
	Path                    string
	Database                *sql.DB
	Vault                   *vault.Vault
	TokenStore              *tokens.Store
	ConfiguredGatewaySecret string
	Registry                connectors.Catalog
	AdapterRegistry         connectorapi.Catalog
	RuntimeInstanceID       func() (string, error)
}

func Adopt(ctx context.Context, input AdoptInput) (State, error) {
	if input.Database == nil || input.Vault == nil || input.Registry == nil || input.AdapterRegistry == nil || input.RuntimeInstanceID == nil {
		return State{}, fmt.Errorf("adopt workspace runtime: required composition dependency is unavailable")
	}
	identityState, err := identity.Adopt(
		ctx, input.Database, input.Vault, input.ConfiguredGatewaySecret, input.RuntimeInstanceID,
	)
	if err != nil {
		return State{}, err
	}
	return State{
		ID: input.ID, Path: input.Path, Database: input.Database,
		Registry: input.Registry, AdapterRegistry: input.AdapterRegistry,
		TokenStore: input.TokenStore, Identity: identityState,
	}, nil
}

func Open(ctx context.Context, input OpenInput) (State, error) {
	if input.Registry == nil || input.AdapterRegistry == nil {
		return State{}, fmt.Errorf("open workspace runtime: connector registries are required")
	}
	ownership, err := db.AcquireDatabaseOwnership(input.Path)
	if err != nil {
		return State{}, err
	}
	owned := true
	defer func() {
		if owned {
			_ = ownership.Close()
		}
	}()
	existingDatabase := db.Exists(input.Path)
	snapshotsBeforeOpen := preMigrationSnapshotSet(input.Path)
	if existingDatabase {
		if err := db.ValidateEncrypted(input.Path, input.Password); err != nil {
			return State{}, fmt.Errorf("%w: encrypted database validation failed", workspacelifecycle.ErrAuthentication)
		}
	}
	database, err := db.OpenEncrypted(input.Path, input.Password)
	if err != nil {
		if existingDatabase {
			return State{}, initializationError(input.Path, err, snapshotsBeforeOpen)
		}
		return State{}, err
	}
	identityState, err := identity.Initialize(ctx, database, input.ConfiguredGatewaySecret)
	if err != nil {
		_ = database.Close()
		if existingDatabase {
			return State{}, initializationError(input.Path, err, snapshotsBeforeOpen)
		}
		return State{}, err
	}
	owned = false
	return State{
		ID: input.ID, Path: input.Path, Database: database, Ownership: ownership,
		Registry: input.Registry, AdapterRegistry: input.AdapterRegistry, Identity: identityState,
	}, nil
}

func initializationError(path string, cause error, snapshotsBeforeOpen map[string]struct{}) error {
	err := fmt.Errorf("%w: %w", workspacelifecycle.ErrInitialization, cause)
	matches, globErr := filepath.Glob(path + ".pre-migration-v*.aipdb")
	if globErr != nil {
		return err
	}
	created := make([]string, 0, len(matches))
	for _, match := range matches {
		if _, existed := snapshotsBeforeOpen[match]; !existed {
			created = append(created, match)
		}
	}
	if len(created) == 0 {
		return err
	}
	sort.Strings(created)
	return fmt.Errorf("%w; encrypted pre-migration snapshot retained at %s", err, created[len(created)-1])
}

func preMigrationSnapshotSet(path string) map[string]struct{} {
	matches, _ := filepath.Glob(path + ".pre-migration-v*.aipdb")
	set := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		set[match] = struct{}{}
	}
	return set
}
