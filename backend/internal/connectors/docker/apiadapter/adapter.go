// Package apiadapter registers Docker connector runtime adapters.
package apiadapter

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	dockerconnector "github.com/aipermission/aipermission/backend/internal/connectors/docker"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/console"
)

type adapter struct{}

func New() connectorapi.Adapter {
	return adapter{}
}

func (adapter) LiveConsoleCapabilityKind() string {
	return connectortargets.RuntimeCapabilityLiveConsole
}

func (adapter) LiveConsoleTargetRef(ctx context.Context, runtime connectorapi.LiveConsoleRuntime, runtimeID int64) (string, error) {
	target, profile, surface, err := dockerTargetProfileByRuntimeID(ctx, runtime, runtimeID)
	if err != nil {
		return "", err
	}
	if surface.ConnectorKind != dockerconnector.Kind || surface.CapabilityKind != connectortargets.RuntimeCapabilityLiveConsole {
		return "", connectortargets.ErrRuntimeSurfaceNotFound
	}
	return connectortargets.ConnectorTargetRef(target.ConnectorKind, target.ID, profile.ID), nil
}

func (adapter) LiveConsoleTargetMetadata(target connectors.TargetView, profile connectors.CredentialProfileView) map[string]any {
	dockerCommand, err := dockerconnector.DockerCommand(target)
	metadata := map[string]any{
		"label":              target.Name,
		"connector":          dockerconnector.Kind,
		"profile":            profile.Label,
		"transport":          strings.TrimSpace(stringConfigValue(target.Config, "transport_target_ref")),
		"docker_command":     dockerCommand,
		"container_scope":    strings.TrimSpace(stringConfigValue(profile.Public, "scope_mode")),
		"allowed_patterns":   strings.TrimSpace(stringConfigValue(profile.Public, "allowed_patterns")),
		"allowed_containers": strings.TrimSpace(stringConfigValue(profile.Public, "allowed_containers")),
	}
	if err != nil {
		metadata["docker_command_error"] = err.Error()
	}
	return metadata
}

func (adapter) OpenLiveConsole(ctx context.Context, server connectorapi.LiveConsoleGateway, runtime connectorapi.LiveConsoleRuntime, request console.RuntimeOpenRequest) (*console.RuntimeSession, error) {
	target, profile, surface, err := dockerTargetProfileByRuntimeID(ctx, runtime, request.RuntimeID)
	if err != nil {
		return nil, err
	}
	if surface.ConnectorKind != dockerconnector.Kind || surface.CapabilityKind != connectortargets.RuntimeCapabilityLiveConsole {
		return nil, connectortargets.ErrRuntimeSurfaceNotFound
	}
	containerRef := strings.TrimSpace(stringParam(request.Params, "container"))
	if containerRef == "" {
		return nil, errors.New("docker container is required")
	}
	if !dockerconnector.ValidContainerRef(containerRef) {
		return nil, errors.New("docker container must be a name or ID without shell syntax")
	}
	if !dockerconnector.ProfileAllowsContainerRef(profile, containerRef) {
		return nil, fmt.Errorf("%w: %s", dockerconnector.ErrScopeDenied, containerRef)
	}
	transportRef := strings.TrimSpace(stringConfigValue(target.Config, "transport_target_ref"))
	if transportRef == "" {
		return nil, fmt.Errorf("%w: transport_target_ref is required", dockerconnector.ErrInvalidConfig)
	}
	dockerCommand, err := dockerconnector.DockerShellCommand(target)
	if err != nil {
		return nil, err
	}
	command := dockerExecShellCommand(dockerCommand, containerRef)
	return server.ConnectorOpenLiveConsole(ctx, transportRef, request.Rows, request.Cols, map[string]any{"force_shell_command": command})
}

func dockerTargetProfileByRuntimeID(ctx context.Context, runtime connectorapi.LiveConsoleRuntime, runtimeID int64) (connectors.TargetView, connectors.CredentialProfileView, connectortargets.RuntimeSurface, error) {
	return runtime.TargetProfileByRuntimeID(ctx, runtimeID)
}

func dockerExecShellCommand(dockerCommand string, containerRef string) string {
	dockerCommand = strings.TrimSpace(dockerCommand)
	if dockerCommand == "" {
		dockerCommand = "docker"
	}
	shellProbe := "if command -v bash >/dev/null 2>&1; then exec bash -l; fi; exec sh"
	identityProbe := fmt.Sprintf("__aip_docker_version=$(%s version --format '{{.Server.Version}}' 2>/dev/null) && test -n \"$__aip_docker_version\"", dockerCommand)
	return fmt.Sprintf("%s || { printf 'Docker identity probe failed.\\n' >&2; exit 127; }; %s exec -it -- %s sh -lc %s", identityProbe, dockerCommand, shellQuote(containerRef), shellQuote(shellProbe))
}

func stringParam(params map[string]any, key string) string {
	if params == nil {
		return ""
	}
	value, ok := params[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func stringConfigValue(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return fmt.Sprint(typed)
	}
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
