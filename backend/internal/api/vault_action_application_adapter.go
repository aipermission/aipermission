package api

import (
	"context"
	"errors"
	"strings"
	"time"

	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
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
	permission, err := port.server.connectorCatalog(port.runtime).ActionPermission(ctx, tokenID, targetID, profileID, action, time.Now().UTC())
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
	items, err := adapter.ExpectedLiveConsolePeerIdentities(ctx, port.server.connectorPortsApplication().PeerGateway(), port.server.connectorLiveRuntime(port.runtime, surface.ConnectorKind), surface.ID)
	if err != nil {
		return gatewayvault.PeerIdentityExpectation{}, err
	}
	return gatewayvault.PeerIdentityExpectation{Items: items, Required: capability.SessionEnvironmentPeerIdentityRequired()}, nil
}

func (s *Server) vaultActionApplication(runtime databaseRuntime) (*gatewayvault.VaultActionRuntime, error) {
	return s.vaultApplication().ActionRuntime(s.vaultRuntime(runtime))
}
