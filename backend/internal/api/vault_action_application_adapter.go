package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	projectstore "github.com/aipermission/aipermission/backend/internal/projects"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
	"github.com/aipermission/aipermission/backend/internal/vaultactions"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

type vaultActionProjectPort struct{ database *sql.DB }

func (p vaultActionProjectPort) ResolveRef(ctx context.Context, ref string) (vaultactions.Project, bool, error) {
	project, err := projectstore.NewStore(p.database).ResolveRef(ctx, ref)
	if errors.Is(err, projectstore.ErrNotFound) {
		return vaultactions.Project{}, false, nil
	}
	if err != nil {
		return vaultactions.Project{}, false, err
	}
	return vaultactions.Project{ID: project.ID, Name: project.Name, Slug: project.Slug}, true, nil
}

func (p vaultActionProjectPort) TokenCanAccess(ctx context.Context, tokenID, projectID int64) (bool, error) {
	return projectstore.NewStore(p.database).TokenCanAccessProject(ctx, tokenID, projectID)
}

type vaultActionItemPort struct{ store *projectvault.Store }

func (p vaultActionItemPort) SnapshotSession(ctx context.Context, selections []projectvault.SessionSelection) (projectvault.SessionResolution, error) {
	return p.store.SnapshotSession(ctx, selections)
}

func (p vaultActionItemPort) ResolveSession(ctx context.Context, selections []projectvault.SessionSelection) (projectvault.SessionResolution, error) {
	return p.store.ResolveSession(ctx, selections)
}

func (p vaultActionItemPort) RevalidateSession(ctx context.Context, items []projectvault.SessionItem) error {
	return p.store.RevalidateSession(ctx, items)
}

func (p vaultActionItemPort) RecordSessionItems(ctx context.Context, sessionID int64, items []projectvault.SessionItem) error {
	return p.store.RecordSessionItems(ctx, sessionID, items)
}

func (p vaultActionItemPort) MarkSessionItemsUsed(ctx context.Context, items []projectvault.SessionItem) error {
	return p.store.MarkSessionItemsUsed(ctx, items)
}

func (p vaultActionItemPort) Create(ctx context.Context, tx *sql.Tx, input projectvault.CreateInput) (projectvault.Item, error) {
	return p.store.WithTx(tx).Create(ctx, input)
}

func (p vaultActionItemPort) Delete(ctx context.Context, id, valueVersion, metadataRevision int64) error {
	return p.store.Delete(ctx, id, valueVersion, metadataRevision)
}

type vaultActionConnectorPort struct {
	server  *Server
	runtime *databaseRuntime
}

func (p vaultActionConnectorPort) SessionEnvironmentVersion(ctx context.Context, runtimeID int64) (string, error) {
	return sessionEnvironmentCapabilityVersion(ctx, p.server, p.runtime, runtimeID)
}

func (p vaultActionConnectorPort) LiveConsolePermission(
	ctx context.Context,
	tokenID, targetID, profileID int64,
	connectorKind string,
) (connectortargets.ActionPermission, string, error) {
	liveConsole, ok := p.server.connectorAPIAdapterFor(connectorKind).(connectorapi.LiveConsoleAdapter)
	if !ok {
		return connectortargets.ActionPermission{}, "", errors.New("this connector does not expose a live console action")
	}
	actionName := strings.TrimSpace(liveConsole.LiveConsoleActionName())
	if actionName == "" {
		return connectortargets.ActionPermission{}, "", errors.New("this connector has an invalid live console action")
	}
	permission, err := connectortargets.NewStore(p.runtime.Storage.Database).GetActionPermission(
		ctx, tokenID, targetID, profileID, actionName, time.Now().UTC(),
	)
	if err != nil || (permission.ExecutionRule != connectortargets.ActionPermissionAlwaysRun &&
		permission.ExecutionRule != connectortargets.ActionPermissionApprovalRequired) {
		return connectortargets.ActionPermission{}, "", errors.New("Vault session apply requires an active Prompt or Always connector action permission")
	}
	return permission, actionName, nil
}

func (p vaultActionConnectorPort) ExpectedPeerIdentities(
	ctx context.Context,
	surface connectortargets.RuntimeSurface,
) (vaultactions.PeerIdentityExpectation, error) {
	capability, err := sessionEnvironmentCapabilityFor(ctx, p.server, p.runtime, surface.ID)
	if err != nil {
		return vaultactions.PeerIdentityExpectation{}, err
	}
	adapter, _ := p.server.connectorAPIAdapterFor(surface.ConnectorKind).(connectorapi.LiveConsolePeerIdentityAdapter)
	if adapter == nil {
		if capability.SessionEnvironmentPeerIdentityRequired() {
			return vaultactions.PeerIdentityExpectation{}, errors.New("this connector requires a peer identity adapter for Vault session environments")
		}
		return vaultactions.PeerIdentityExpectation{}, nil
	}
	items, err := adapter.ExpectedLiveConsolePeerIdentities(
		ctx, connectorPeerGatewayPort{server: p.server}, connectorLiveRuntime(p.runtime, surface.ConnectorKind), surface.ID,
	)
	if err != nil {
		return vaultactions.PeerIdentityExpectation{}, err
	}
	return vaultactions.PeerIdentityExpectation{
		Items: items, Required: capability.SessionEnvironmentPeerIdentityRequired(),
	}, nil
}

type vaultActionDeliveryPort struct{ runtime *databaseRuntime }

func (p vaultActionDeliveryPort) AcquireDelivery(ctx context.Context) (func(), error) {
	if p.runtime == nil {
		return nil, vaultactions.ErrRuntimeUnavailable
	}
	return p.runtime.Security.VaultDelivery.AcquireDelivery(ctx)
}

func (p vaultActionDeliveryPort) AcquireExclusive(ctx context.Context) (func(), error) {
	if p.runtime == nil {
		return nil, vaultactions.ErrRuntimeUnavailable
	}
	return p.runtime.Security.VaultDelivery.AcquireExclusive(ctx)
}

type vaultActionMutationPort struct {
	server  *Server
	runtime *databaseRuntime
}

func (p vaultActionMutationPort) WithMutation(
	ctx context.Context,
	tokenID int64,
	action string,
	payload func() any,
	mutate func(*sql.Tx) error,
) error {
	if p.server == nil || p.runtime == nil {
		return vaultactions.ErrRuntimeUnavailable
	}
	return p.server.withAuditedMutation(ctx, p.runtime, "mcp", &tokenID, 0, action, payload, mutate)
}

func (s *Server) vaultActionApplication(runtime *databaseRuntime) (*vaultactions.Runtime, error) {
	if s == nil || runtime == nil || runtime.Storage.Database == nil || runtime.Storage.Vault == nil ||
		runtime.Storage.Tokens == nil || runtime.Connectors.ConsoleSessions == nil || runtime.Security.VaultLeases == nil {
		return nil, vaultactions.ErrRuntimeUnavailable
	}
	itemStore, err := projectvault.NewStore(runtime.Storage.Database, runtime.Storage.Vault, runtime.WorkspaceUUID)
	if err != nil {
		return nil, fmt.Errorf("initialize Vault action item store: %w", err)
	}
	owner, err := vaultactions.NewRuntime(vaultactions.Dependencies{
		Database: runtime.Storage.Database, Tokens: runtime.Storage.Tokens,
		Projects:      vaultActionProjectPort{database: runtime.Storage.Database},
		SessionItems:  vaultActionItemPort{store: itemStore},
		ItemMutations: vaultActionItemPort{store: itemStore},
		Sessions:      runtime.Connectors.ConsoleSessions, Leases: runtime.Security.VaultLeases,
		PersistedLeases: vaultsessions.NewPersistence(runtime.Storage.Database),
		Connector:       vaultActionConnectorPort{server: s, runtime: runtime},
		Delivery:        vaultActionDeliveryPort{runtime: runtime},
		Mutations:       vaultActionMutationPort{server: s, runtime: runtime},
		WorkspaceID:     runtime.WorkspaceUUID, RuntimeInstanceID: runtime.RuntimeInstanceID,
		MCPStarted: runtime.IsMCPStarted,
		AllowGenerate: func(tokenID int64) bool {
			return s.vaultGenerateLimiter != nil && s.vaultGenerateLimiter.Allow(
				fmt.Sprintf("vault-generate:%s:%d", runtime.ID, tokenID),
			)
		},
	})
	if err != nil {
		return nil, fmt.Errorf("initialize Vault action application: %w", err)
	}
	return owner, nil
}
