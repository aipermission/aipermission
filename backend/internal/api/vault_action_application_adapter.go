package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
	gatewayvault "github.com/aipermission/aipermission/backend/internal/gatewayvault"
)

type vaultActionConnectorPort struct {
	server  *Server
	runtime databaseRuntime
}

func (port vaultActionConnectorPort) SessionEnvironmentVersion(ctx context.Context, runtimeID int64) (string, error) {
	return sessionEnvironmentCapabilityVersion(ctx, port.server, port.runtime, runtimeID)
}

func (port vaultActionConnectorPort) LiveConsolePermission(ctx context.Context, tokenID, targetID, profileID int64, kind string) (connectormgmt.ActionPermission, string, error) {
	liveConsole, ok := port.server.connectorAPIAdapterFor(kind).(connectorapi.LiveConsoleAdapter)
	if !ok {
		return connectormgmt.ActionPermission{}, "", errors.New("this connector does not expose a live console action")
	}
	action := strings.TrimSpace(liveConsole.LiveConsoleActionName())
	if action == "" {
		return connectormgmt.ActionPermission{}, "", errors.New("this connector has an invalid live console action")
	}
	permission, err := connectormgmt.NewStore(port.runtime.StoragePort().DatabaseHandle()).GetActionPermission(ctx, tokenID, targetID, profileID, action, time.Now().UTC())
	if err != nil || (permission.ExecutionRule != connectormgmt.ActionPermissionAlwaysRun && permission.ExecutionRule != connectormgmt.ActionPermissionApprovalRequired) {
		return connectormgmt.ActionPermission{}, "", errors.New("Vault session apply requires an active Prompt or Always connector action permission")
	}
	return permission, action, nil
}

func (port vaultActionConnectorPort) ExpectedPeerIdentities(ctx context.Context, surface connectormgmt.RuntimeSurface) (gatewayvault.PeerIdentityExpectation, error) {
	capability, err := sessionEnvironmentCapabilityFor(ctx, port.server, port.runtime, surface.ID)
	if err != nil {
		return gatewayvault.PeerIdentityExpectation{}, err
	}
	adapter, _ := port.server.connectorAPIAdapterFor(surface.ConnectorKind).(connectorapi.LiveConsolePeerIdentityAdapter)
	if adapter == nil {
		if capability.SessionEnvironmentPeerIdentityRequired() {
			return gatewayvault.PeerIdentityExpectation{}, errors.New("this connector requires a peer identity adapter for Vault session environments")
		}
		return gatewayvault.PeerIdentityExpectation{}, nil
	}
	items, err := adapter.ExpectedLiveConsolePeerIdentities(ctx, port.server.connectorPortsApplication().PeerGateway(), connectorLiveRuntime(port.runtime, surface.ConnectorKind), surface.ID)
	if err != nil {
		return gatewayvault.PeerIdentityExpectation{}, err
	}
	return gatewayvault.PeerIdentityExpectation{Items: items, Required: capability.SessionEnvironmentPeerIdentityRequired()}, nil
}

func (s *Server) vaultActionApplication(runtime databaseRuntime) (*gatewayvault.VaultActionRuntime, error) {
	component := s.vaultApplication()
	s.configureVaultActions(component)
	return component.ActionRuntime(runtime)
}

func (s *Server) configureVaultActions(component *gatewayvault.Application) {
	component.ConfigureActions(gatewayvault.ActionDependencies{
		Connector: func(runtime gatewayinfra.Runtime) gatewayvault.VaultConnectorPort {
			return vaultActionConnectorPort{server: s, runtime: runtime}
		},
		Mutate: func(ctx context.Context, runtime gatewayinfra.Runtime, tokenID int64, action string, payload func() any, mutate func(*sql.Tx) error) error {
			return s.withAuditedMutation(ctx, runtime, "mcp", &tokenID, 0, action, payload, mutate)
		},
		AllowGenerate: func(runtime gatewayinfra.Runtime, tokenID int64) bool {
			return s.controlState.VaultGenerateLimiter != nil && s.controlState.VaultGenerateLimiter.Allow(fmt.Sprintf("vault-generate:%s:%d", runtime.DatabaseIdentifier(), tokenID))
		},
	})
}
