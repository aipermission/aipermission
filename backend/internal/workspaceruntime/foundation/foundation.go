package foundation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/db"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	"github.com/aipermission/aipermission/backend/internal/tokens"
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

func Open(ctx context.Context, input OpenInput) (_ State, resultErr error) {
	if input.Registry == nil || input.AdapterRegistry == nil {
		return State{}, fmt.Errorf("open workspace runtime: connector registries are required")
	}
	ownership, err := db.AcquireDatabaseOwnership(input.Path)
	if err != nil {
		return State{}, err
	}
	resources := failedOpenResources{ownership: ownership}
	owned := true
	defer func() {
		if owned {
			resultErr = errors.Join(resultErr, resources.Close())
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
	resources.database = database
	identityState, err := identity.Initialize(ctx, database, input.ConfiguredGatewaySecret)
	if err != nil {
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
