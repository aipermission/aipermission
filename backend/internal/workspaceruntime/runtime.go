package workspaceruntime

import (
	"database/sql"
	"sync"

	"github.com/aipermission/aipermission/backend/internal/workspacelifecycle"
	connectorstate "github.com/aipermission/aipermission/backend/internal/workspaceruntime/connectors"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime/foundation"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime/observation"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime/operations"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime/security"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime/storage"
)

type Runtime struct {
	ID                string
	Path              string
	GatewaySecret     string
	WorkspaceUUID     string
	UIRetryIdentity   string
	RuntimeInstanceID string
	ActionIdentityKey []byte
	Storage           storage.State
	Connectors        connectorstate.State
	Operations        operations.State
	Security          security.State
	Observation       observation.State
	identityMu        sync.Mutex
}

func New(state foundation.State) *Runtime {
	return &Runtime{
		ID: state.ID, Path: state.Path,
		GatewaySecret:     state.Identity.GatewaySecret,
		WorkspaceUUID:     state.Identity.WorkspaceUUID,
		UIRetryIdentity:   state.Identity.UIRetryIdentity,
		RuntimeInstanceID: state.Identity.RuntimeInstanceID,
		ActionIdentityKey: state.Identity.ActionIdentityKey,
		Storage: storage.New(
			state.Database, state.Identity.Vault, state.TokenStore, state.Identity.WorkspaceUUID, state.Ownership,
		),
		Connectors: connectorstate.New(
			state.Registry, state.AdapterRegistry, state.Database, state.Identity.Vault, state.Identity.WorkspaceUUID,
		),
		Operations: operations.New(),
		Security:   security.New(state.Database),
	}
}

func (r *Runtime) WorkspaceIdentity() workspacelifecycle.Identity {
	if r == nil {
		return workspacelifecycle.Identity{}
	}
	return workspacelifecycle.Identity{
		ID: r.ID, Path: r.Path, RetryIdentity: r.UIRetryIdentity,
	}
}

func (r *Runtime) WorkspaceDatabase() *sql.DB {
	if r == nil {
		return nil
	}
	return r.Storage.Database
}

func (r *Runtime) EnsureIdentity(
	ensureWorkspace func(*sql.DB) (string, error),
	newRuntimeInstance func() (string, error),
) error {
	if r == nil {
		return nil
	}
	r.identityMu.Lock()
	defer r.identityMu.Unlock()
	var err error
	if r.WorkspaceUUID == "" {
		r.WorkspaceUUID, err = ensureWorkspace(r.Storage.Database)
		if err != nil {
			return err
		}
	}
	if r.RuntimeInstanceID == "" {
		r.RuntimeInstanceID, err = newRuntimeInstance()
	}
	return err
}

func (r *Runtime) IsMCPStarted() bool {
	return r != nil && r.Security.Runtime.MCPStarted()
}

func (r *Runtime) SetMCPStarted(enabled bool) {
	if r != nil {
		r.Security.Runtime.SetMCPStarted(enabled)
	}
}
