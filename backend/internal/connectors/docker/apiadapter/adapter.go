// Package apiadapter registers Docker connector runtime adapters.
package apiadapter

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	dockerconnector "github.com/aipermission/aipermission/backend/internal/connectors/docker"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

type adapter struct{}

func New() connectorapi.Adapter {
	return adapter{}
}

func (adapter) LiveConsoleCapabilityKind() string {
	return connectortargets.RuntimeCapabilityLiveConsole
}

func (adapter) LiveConsoleTargetRef(ctx context.Context, runtime connectorapi.LiveConsoleRuntime, runtimeID int64) (string, error) {
	target, profile, surface, err := runtime.TargetProfileByRuntimeID(ctx, runtimeID)
	if err != nil {
		return "", err
	}
	if surface.ConnectorKind != dockerconnector.Kind || surface.CapabilityKind != connectortargets.RuntimeCapabilityLiveConsole {
		return "", connectortargets.ErrRuntimeSurfaceNotFound
	}
	return connectors.FormatTargetRef(target.ConnectorKind, target.ID, profile.ID), nil
}

func (adapter) LiveConsoleTargetMetadata(target connectors.TargetView, profile connectors.CredentialProfileView) map[string]any {
	dockerCommand, err := dockerconnector.DockerCommand(target)
	metadata := map[string]any{
		"label":              target.Name,
		"connector":          dockerconnector.Kind,
		"profile":            profile.Label,
		"transport":          strings.TrimSpace(connectors.StringMapValue(target.Config, "transport_target_ref")),
		"docker_command":     dockerCommand,
		"container_scope":    strings.TrimSpace(connectors.StringMapValue(profile.Public, "scope_mode")),
		"allowed_patterns":   strings.TrimSpace(connectors.StringMapValue(profile.Public, "allowed_patterns")),
		"allowed_containers": strings.TrimSpace(connectors.StringMapValue(profile.Public, "allowed_containers")),
	}
	if err != nil {
		metadata["docker_command_error"] = err.Error()
	}
	return metadata
}

func (adapter) OpenLiveConsole(ctx context.Context, server connectorapi.LiveConsoleGateway, runtime connectorapi.LiveConsoleRuntime, request connectorapi.LiveConsoleOpenRequest) (*connectorapi.LiveConsoleSession, error) {
	target, profile, surface, err := runtime.TargetProfileByRuntimeID(ctx, request.RuntimeID)
	if err != nil {
		return nil, err
	}
	if surface.ConnectorKind != dockerconnector.Kind || surface.CapabilityKind != connectortargets.RuntimeCapabilityLiveConsole {
		return nil, connectortargets.ErrRuntimeSurfaceNotFound
	}
	containerRef := strings.TrimSpace(connectors.StringMapValue(request.Params, "container"))
	if containerRef == "" {
		return nil, errors.New("docker container is required")
	}
	if !dockerconnector.ValidContainerRef(containerRef) {
		return nil, errors.New("docker container must be a name or ID without shell syntax")
	}
	transportRef := strings.TrimSpace(connectors.StringMapValue(target.Config, "transport_target_ref"))
	if transportRef == "" {
		return nil, fmt.Errorf("%w: transport_target_ref is required", dockerconnector.ErrInvalidConfig)
	}
	dockerCommand, err := dockerconnector.DockerShellCommand(target)
	if err != nil {
		return nil, err
	}
	inventoryCommand, err := dockerconnector.ContainerInventoryCommand(target)
	if err != nil {
		return nil, err
	}
	inventory, err := server.ConnectorRunCommand(ctx, connectors.CommandRunRequest{
		SourceTargetRef:    target.Ref,
		Mode:               dockerconnector.ConnectionMode(target),
		TransportTargetRef: transportRef,
		Command:            inventoryCommand,
		TimeoutSeconds:     20,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve docker container: %w", err)
	}
	if inventory.ExitCode != 0 {
		return nil, fmt.Errorf("resolve docker container: inventory command exited with code %d", inventory.ExitCode)
	}
	container, err := dockerconnector.ResolveProfileContainer(profile, containerRef, inventory.Stdout)
	if err != nil {
		return nil, err
	}
	command := dockerExecShellCommand(dockerCommand, container.ID)
	return server.ConnectorOpenLiveConsole(ctx, transportRef, request.Rows, request.Cols, map[string]any{"force_shell_command": command})
}

func dockerExecShellCommand(dockerCommand string, containerRef string) string {
	dockerCommand = strings.TrimSpace(dockerCommand)
	if dockerCommand == "" {
		dockerCommand = "docker"
	}
	identityProbe := fmt.Sprintf("__aip_docker_version=$(%s version --format '{{.Server.Version}}' 2>/dev/null) && test -n \"$__aip_docker_version\"", dockerCommand)
	return fmt.Sprintf("%s || { printf 'Docker identity probe failed.\\n' >&2; exit 127; }; %s exec -it -- %s sh -lc %s", identityProbe, dockerCommand, connectors.QuoteShellArgument(containerRef), connectors.QuoteShellArgument(connectors.InteractiveShellProbe))
}
