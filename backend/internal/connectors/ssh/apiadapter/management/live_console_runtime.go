package management

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	sshconnector "github.com/aipermission/aipermission/backend/internal/connectors/ssh"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

func (Management) LiveConsoleCapabilityKind() string {
	return connectortargets.RuntimeCapabilityLiveConsole
}

func (Management) LiveConsoleTargetRef(ctx context.Context, runtime connectorapi.LiveConsoleRuntime, runtimeID int64) (string, error) {
	contextValue, _ := ctx.(context.Context)
	if contextValue == nil {
		contextValue = context.Background()
	}
	target, profile, surface, err := runtime.TargetProfileByRuntimeID(contextValue, runtimeID)
	if err != nil {
		return "", err
	}
	if surface.ConnectorKind != sshconnector.Kind || surface.CapabilityKind != connectortargets.RuntimeCapabilityLiveConsole {
		return "", connectortargets.ErrRuntimeSurfaceNotFound
	}
	return connectors.FormatTargetRef(target.ConnectorKind, target.ID, profile.ID), nil
}

func (Management) LiveConsoleTargetMetadata(target connectors.TargetView, profile connectors.CredentialProfileView) map[string]any {
	metadata := map[string]any{}
	if host := stringConfigValue(target.Config, "host"); host != "" {
		metadata["host"] = host
	}
	if port := intConfigValue(target.Config, "port", 22); port > 0 {
		metadata["port"] = port
	}
	if username := stringConfigValue(profile.Public, "username"); username != "" {
		metadata["username"] = username
	}
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}
