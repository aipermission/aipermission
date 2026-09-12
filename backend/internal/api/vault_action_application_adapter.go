package api

import (
	"context"
	"errors"
	"time"

	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
	gatewayvault "github.com/aipermission/aipermission/backend/internal/gatewayvault"
)

type vaultActionConnectorPort struct {
	server  *Server
	runtime *gatewayinfra.WorkspaceHandle
}

func (port vaultActionConnectorPort) SessionEnvironmentVersion(ctx context.Context, runtimeID int64) (string, error) {
	return sessionEnvironmentCapabilityVersion(ctx, port.server, port.runtime, runtimeID)
}

func (port vaultActionConnectorPort) LiveConsolePermission(ctx context.Context, tokenID, targetID, profileID int64, kind string) (gatewayvault.ConnectorPermission, string, error) {
	action, ok := port.server.connectorRuntime.LiveConsoleActionName(kind)
	if !ok {
		return gatewayvault.ConnectorPermission{}, "", errors.New("this connector does not expose a live console action")
	}
	permission, err := port.server.connectorCatalog(port.runtime).ActionPermission(ctx, tokenID, targetID, profileID, action, time.Now().UTC())
	return gatewayvault.ConnectorPermission{
		ExecutionRule: string(permission.ExecutionRule), ExpiresAt: permission.ExpiresAt, UpdatedAt: permission.UpdatedAt,
	}, action, err
}

func (port vaultActionConnectorPort) ExpectedPeerIdentities(ctx context.Context, surface gatewayvault.ConnectorRuntimeSurface) (gatewayvault.PeerIdentityExpectation, error) {
	capability, err := sessionEnvironmentCapabilityFor(ctx, port.server, port.runtime, surface.ID)
	if err != nil {
		return gatewayvault.PeerIdentityExpectation{}, err
	}
	items, supported, err := port.server.connectorRuntime.ExpectedLiveConsolePeerIdentities(ctx, port.runtime, surface.ConnectorKind, surface.ID)
	if !supported {
		if capability.SessionEnvironmentPeerIdentityRequired() {
			return gatewayvault.PeerIdentityExpectation{}, errors.New("this connector requires a peer identity adapter for Vault session environments")
		}
		return gatewayvault.PeerIdentityExpectation{}, nil
	}
	if err != nil {
		return gatewayvault.PeerIdentityExpectation{}, err
	}
	return gatewayvault.PeerIdentityExpectation{Items: items, Required: capability.SessionEnvironmentPeerIdentityRequired()}, nil
}

func (s *Server) vaultActionApplication(runtime *gatewayinfra.WorkspaceHandle) (gatewayvault.VaultActionApplication, error) {
	return s.vaultOwner.VaultActionApplication(runtime, s.vaultApplication(), s.vaultRuntimePorts(runtime))
}
