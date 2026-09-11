package workspaceruntime

import (
	"errors"

	"github.com/aipermission/aipermission/backend/internal/actions"
	connectorstate "github.com/aipermission/aipermission/backend/internal/gatewayworkspace/runtime/connectors"
	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace/runtime/observation"
	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace/runtime/security"
	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace/runtime/storage"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime/foundation"
)

func TagActionIdentity(runtime *Runtime, canonical []byte) (string, error) {
	if runtime == nil {
		return "", errors.New("workspace action identity is unavailable")
	}
	return actions.IdentityTag(runtime.ActionIdentityKey, canonical)
}

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
	Security          security.State
	Observation       observation.State
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
		Security: security.New(state.Database),
	}
}
