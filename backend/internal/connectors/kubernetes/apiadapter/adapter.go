// Package apiadapter registers Kubernetes connector runtime adapters.
package apiadapter

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	kubernetesconnector "github.com/aipermission/aipermission/backend/internal/connectors/kubernetes"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/console"
)

var kubeConsoleNamePattern = regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)

type adapter struct{}

func New() connectorapi.Adapter {
	return adapter{}
}

func (adapter) LiveConsoleCapabilityKind() string {
	return connectortargets.RuntimeCapabilityLiveConsole
}

func (adapter) LiveConsoleTargetRef(ctx context.Context, runtime connectorapi.LiveConsoleRuntime, runtimeID int64) (string, error) {
	target, profile, surface, err := kubernetesTargetProfileByRuntimeID(ctx, runtime, runtimeID)
	if err != nil {
		return "", err
	}
	if surface.ConnectorKind != kubernetesconnector.Kind || surface.CapabilityKind != connectortargets.RuntimeCapabilityLiveConsole {
		return "", connectortargets.ErrRuntimeSurfaceNotFound
	}
	return connectortargets.ConnectorTargetRef(target.ConnectorKind, target.ID, profile.ID), nil
}

func (adapter) LiveConsoleTargetMetadata(target connectors.TargetView, profile connectors.CredentialProfileView) map[string]any {
	kubectl, _ := kubernetesconnector.KubectlCommand(target)
	return map[string]any{
		"label":           target.Name,
		"connector":       kubernetesconnector.Kind,
		"profile":         profile.Label,
		"transport":       strings.TrimSpace(stringConfigValue(target.Config, "transport_target_ref")),
		"kubectl":         kubectl,
		"context":         strings.TrimSpace(stringConfigValue(target.Config, "context")),
		"default_ns":      strings.TrimSpace(stringConfigValue(target.Config, "default_namespace")),
		"namespace_scope": strings.TrimSpace(stringConfigValue(profile.Public, "scope_mode")),
		"namespaces":      strings.TrimSpace(stringConfigValue(profile.Public, "namespaces")),
	}
}

func (adapter) OpenLiveConsole(ctx context.Context, server connectorapi.LiveConsoleGateway, runtime connectorapi.LiveConsoleRuntime, request console.RuntimeOpenRequest) (*console.RuntimeSession, error) {
	target, profile, surface, err := kubernetesTargetProfileByRuntimeID(ctx, runtime, request.RuntimeID)
	if err != nil {
		return nil, err
	}
	if surface.ConnectorKind != kubernetesconnector.Kind || surface.CapabilityKind != connectortargets.RuntimeCapabilityLiveConsole {
		return nil, connectortargets.ErrRuntimeSurfaceNotFound
	}
	namespace := strings.TrimSpace(stringParam(request.Params, "namespace"))
	pod := strings.TrimSpace(stringParam(request.Params, "pod"))
	container := strings.TrimSpace(stringParam(request.Params, "container"))
	if namespace == "" || pod == "" {
		return nil, errors.New("kubernetes namespace and pod are required")
	}
	for _, value := range []string{namespace, pod, container} {
		if value != "" && !kubeConsoleNamePattern.MatchString(value) {
			return nil, fmt.Errorf("invalid kubernetes object name: %s", value)
		}
	}
	if !kubernetesconnector.ProfileAllowsNamespace(profile, namespace) {
		return nil, fmt.Errorf("%w: %s", kubernetesconnector.ErrScopeDenied, namespace)
	}
	transportRef := strings.TrimSpace(stringConfigValue(target.Config, "transport_target_ref"))
	if transportRef == "" {
		return nil, fmt.Errorf("%w: transport_target_ref is required", kubernetesconnector.ErrInvalidConfig)
	}
	command, err := kubectlExecShellCommand(target, namespace, pod, container)
	if err != nil {
		return nil, err
	}
	return server.ConnectorOpenLiveConsole(ctx, transportRef, request.Rows, request.Cols, map[string]any{"force_shell_command": command})
}

func kubernetesTargetProfileByRuntimeID(ctx context.Context, runtime connectorapi.LiveConsoleRuntime, runtimeID int64) (connectors.TargetView, connectors.CredentialProfileView, connectortargets.RuntimeSurface, error) {
	return runtime.TargetProfileByRuntimeID(ctx, runtimeID)
}

func kubectlExecShellCommand(target connectors.TargetView, namespace string, pod string, container string) (string, error) {
	command, err := kubernetesconnector.KubectlCommand(target)
	if err != nil {
		return "", err
	}
	contextName := strings.TrimSpace(stringConfigValue(target.Config, "context"))
	if contextName != "" {
		command += " --context " + shellQuote(contextName)
	}
	command += " exec -it -n " + shellQuote(namespace) + " " + shellQuote(pod)
	if container != "" {
		command += " -c " + shellQuote(container)
	}
	shellProbe := "if command -v bash >/dev/null 2>&1; then exec bash -l; fi; exec sh"
	return command + " -- sh -lc " + shellQuote(shellProbe), nil
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
