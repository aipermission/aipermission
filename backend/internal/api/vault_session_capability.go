package api

import (
	"context"
	"errors"
	"strings"

	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
	connectors "github.com/aipermission/aipermission/backend/internal/gatewayconnectors"
)

func requireSessionEnvironmentCapability(ctx context.Context, server *Server, runtime databaseRuntime, runtimeID int64) error {
	_, err := sessionEnvironmentCapabilityVersion(ctx, server, runtime, runtimeID)
	return err
}

func sessionEnvironmentCapabilityVersion(ctx context.Context, server *Server, runtime databaseRuntime, runtimeID int64) (string, error) {
	sessionCapability, err := sessionEnvironmentCapabilityFor(ctx, server, runtime, runtimeID)
	if err != nil {
		return "", err
	}
	version := strings.TrimSpace(sessionCapability.SessionEnvironmentVersion())
	if version == "" {
		return "", errors.New("this connector runtime has an invalid Vault session environment version")
	}
	return version, nil
}

func sessionEnvironmentCapabilityFor(ctx context.Context, server *Server, runtime databaseRuntime, runtimeID int64) (connectors.SessionEnvironmentCapability, error) {
	surface, err := connectormgmt.NewStore(runtime.StoragePort().DatabaseHandle()).GetRuntimeSurface(ctx, runtimeID)
	if err != nil {
		return nil, err
	}
	capabilities := connectorRuntimeCapabilitiesFor(surface.ConnectorKind, server, runtime)
	if capabilities == nil {
		return nil, connectors.ErrSessionEnvironmentUnsupported
	}
	capability := capabilities.RuntimeCapability(connectors.SessionEnvironmentCapabilityName)
	sessionCapability, ok := capability.(connectors.SessionEnvironmentCapability)
	if !ok {
		return nil, connectors.ErrSessionEnvironmentUnsupported
	}
	return sessionCapability, nil
}
