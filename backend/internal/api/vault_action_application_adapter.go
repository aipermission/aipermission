package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/applicationvault"
	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/vaultactions"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime"
)

type vaultActionConnectorPort struct {
	server  *Server
	runtime *databaseRuntime
}

func (port vaultActionConnectorPort) SessionEnvironmentVersion(ctx context.Context, runtimeID int64) (string, error) {
	return sessionEnvironmentCapabilityVersion(ctx, port.server, port.runtime, runtimeID)
}

func (port vaultActionConnectorPort) LiveConsolePermission(ctx context.Context, tokenID, targetID, profileID int64, kind string) (connectortargets.ActionPermission, string, error) {
	liveConsole, ok := port.server.connectorAPIAdapterFor(kind).(connectorapi.LiveConsoleAdapter)
	if !ok {
		return connectortargets.ActionPermission{}, "", errors.New("this connector does not expose a live console action")
	}
	action := strings.TrimSpace(liveConsole.LiveConsoleActionName())
	if action == "" {
		return connectortargets.ActionPermission{}, "", errors.New("this connector has an invalid live console action")
	}
	permission, err := connectortargets.NewStore(port.runtime.Storage.Database).GetActionPermission(ctx, tokenID, targetID, profileID, action, time.Now().UTC())
	if err != nil || (permission.ExecutionRule != connectortargets.ActionPermissionAlwaysRun && permission.ExecutionRule != connectortargets.ActionPermissionApprovalRequired) {
		return connectortargets.ActionPermission{}, "", errors.New("Vault session apply requires an active Prompt or Always connector action permission")
	}
	return permission, action, nil
}

func (port vaultActionConnectorPort) ExpectedPeerIdentities(ctx context.Context, surface connectortargets.RuntimeSurface) (vaultactions.PeerIdentityExpectation, error) {
	capability, err := sessionEnvironmentCapabilityFor(ctx, port.server, port.runtime, surface.ID)
	if err != nil {
		return vaultactions.PeerIdentityExpectation{}, err
	}
	adapter, _ := port.server.connectorAPIAdapterFor(surface.ConnectorKind).(connectorapi.LiveConsolePeerIdentityAdapter)
	if adapter == nil {
		if capability.SessionEnvironmentPeerIdentityRequired() {
			return vaultactions.PeerIdentityExpectation{}, errors.New("this connector requires a peer identity adapter for Vault session environments")
		}
		return vaultactions.PeerIdentityExpectation{}, nil
	}
	items, err := adapter.ExpectedLiveConsolePeerIdentities(ctx, port.server.connectorPortsApplication().PeerGateway(), connectorLiveRuntime(port.runtime, surface.ConnectorKind), surface.ID)
	if err != nil {
		return vaultactions.PeerIdentityExpectation{}, err
	}
	return vaultactions.PeerIdentityExpectation{Items: items, Required: capability.SessionEnvironmentPeerIdentityRequired()}, nil
}

func (s *Server) vaultActionApplication(runtime *databaseRuntime) (*vaultactions.Runtime, error) {
	component := s.vaultApplication()
	s.configureVaultActions(component)
	return component.ActionRuntime(runtime)
}

func (s *Server) configureVaultActions(component *applicationvault.Component) {
	component.ConfigureActions(applicationvault.ActionDependencies{
		Connector: func(runtime *workspaceruntime.Runtime) vaultactions.ConnectorPort {
			return vaultActionConnectorPort{server: s, runtime: runtime}
		},
		Mutate: func(ctx context.Context, runtime *workspaceruntime.Runtime, tokenID int64, action string, payload func() any, mutate func(*sql.Tx) error) error {
			return s.withAuditedMutation(ctx, runtime, "mcp", &tokenID, 0, action, payload, mutate)
		},
		AllowGenerate: func(runtime *workspaceruntime.Runtime, tokenID int64) bool {
			return s.controlState.VaultGenerateLimiter != nil && s.controlState.VaultGenerateLimiter.Allow(fmt.Sprintf("vault-generate:%s:%d", runtime.ID, tokenID))
		},
	})
}
