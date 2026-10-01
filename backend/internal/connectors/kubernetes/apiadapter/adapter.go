// Package apiadapter registers Kubernetes connector runtime adapters.
package apiadapter

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	kubernetesconnector "github.com/aipermission/aipermission/backend/internal/connectors/kubernetes"
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
	if surface.ConnectorKind != kubernetesconnector.Kind || surface.CapabilityKind != connectortargets.RuntimeCapabilityLiveConsole {
		return "", connectortargets.ErrRuntimeSurfaceNotFound
	}
	return connectors.FormatTargetRef(target.ConnectorKind, target.ID, profile.ID), nil
}

func (adapter) LiveConsoleTargetMetadata(target connectors.TargetView, profile connectors.CredentialProfileView) map[string]any {
	kubectl, _ := kubernetesconnector.KubectlCommand(target)
	return map[string]any{
		"label":           target.Name,
		"connector":       kubernetesconnector.Kind,
		"profile":         profile.Label,
		"transport":       strings.TrimSpace(connectors.StringMapValue(target.Config, "transport_target_ref")),
		"kubectl":         kubectl,
		"context":         strings.TrimSpace(connectors.StringMapValue(target.Config, "context")),
		"default_ns":      strings.TrimSpace(connectors.StringMapValue(target.Config, "default_namespace")),
		"namespace_scope": strings.TrimSpace(connectors.StringMapValue(profile.Public, "scope_mode")),
		"namespaces":      strings.TrimSpace(connectors.StringMapValue(profile.Public, "namespaces")),
	}
}

func (adapter) OpenLiveConsole(ctx context.Context, server connectorapi.LiveConsoleGateway, runtime connectorapi.LiveConsoleRuntime, request connectorapi.LiveConsoleOpenRequest) (*connectorapi.LiveConsoleSession, error) {
	target, profile, surface, err := runtime.TargetProfileByRuntimeID(ctx, request.RuntimeID)
	if err != nil {
		return nil, err
	}
	if surface.ConnectorKind != kubernetesconnector.Kind || surface.CapabilityKind != connectortargets.RuntimeCapabilityLiveConsole {
		return nil, connectortargets.ErrRuntimeSurfaceNotFound
	}
	namespace := strings.TrimSpace(connectors.StringMapValue(request.Params, "namespace"))
	pod := strings.TrimSpace(connectors.StringMapValue(request.Params, "pod"))
	container := strings.TrimSpace(connectors.StringMapValue(request.Params, "container"))
	if namespace == "" || pod == "" {
		return nil, errors.New("kubernetes namespace and pod are required")
	}
	for _, value := range []string{namespace, pod} {
		if _, err := kubernetesconnector.NormalizeObjectName(value); err != nil {
			return nil, err
		}
	}
	if container != "" {
		if _, err := kubernetesconnector.NormalizeObjectName(container); err != nil {
			return nil, err
		}
	}
	if !kubernetesconnector.ProfileAllowsNamespace(profile, namespace) {
		return nil, fmt.Errorf("%w: %s", kubernetesconnector.ErrScopeDenied, namespace)
	}
	transportRef := strings.TrimSpace(connectors.StringMapValue(target.Config, "transport_target_ref"))
	if transportRef == "" {
		return nil, fmt.Errorf("%w: transport_target_ref is required", kubernetesconnector.ErrInvalidConfig)
	}
	command, err := kubectlExecShellCommand(target, namespace, pod, container)
	if err != nil {
		return nil, err
	}
	return server.ConnectorOpenLiveConsole(ctx, transportRef, request.Rows, request.Cols, map[string]any{"force_shell_command": command})
}

func kubectlExecShellCommand(target connectors.TargetView, namespace string, pod string, container string) (string, error) {
	command, err := kubernetesconnector.KubectlCommand(target)
	if err != nil {
		return "", err
	}
	contextName := strings.TrimSpace(connectors.StringMapValue(target.Config, "context"))
	if contextName != "" {
		command += " --context " + connectors.QuoteShellArgument(contextName)
	}
	command += " exec -it -n " + connectors.QuoteShellArgument(namespace) + " " + connectors.QuoteShellArgument(pod)
	if container != "" {
		command += " -c " + connectors.QuoteShellArgument(container)
	}
	return command + " -- sh -lc " + connectors.QuoteShellArgument(connectors.InteractiveShellProbe), nil
}
