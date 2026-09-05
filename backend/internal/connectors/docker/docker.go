// Package dockerconnector defines the Docker connector contract.
package dockerconnector

import (
	"context"
	"errors"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

const (
	Kind    = "docker"
	Label   = "Docker"
	Version = "0.2"

	ActionVersion          = "docker_version"
	ActionListContainers   = "list_containers"
	ActionListImages       = "list_images"
	ActionListNetworks     = "list_networks"
	ActionListVolumes      = "list_volumes"
	ActionInspectContainer = "inspect_container"
	ActionContainerLogs    = "container_logs"
	ActionContainerExec    = "container_exec"
	ActionStartContainer   = "start_container"
	ActionStopContainer    = "stop_container"
	ActionRestartContainer = "restart_container"

	defaultLogTail       = 200
	maxLogTail           = 2000
	maxLogBytes          = 256 << 10
	maxExecCommandLen    = 8000
	maxExecOutputBytes   = 256 << 10
	maxInspectBytes      = 512 << 10
	maxDockerReasonLen   = 2000
	defaultDockerCommand = "docker"
)

var (
	ErrUnsupportedAction = errors.New("unsupported docker connector action")
	ErrMissingTransport  = errors.New("docker connector command transport is unavailable")
	ErrInvalidConfig     = errors.New("docker connector target config is invalid")
	ErrScopeDenied       = errors.New("docker container is outside this credential profile scope")
	errInvalidIdentity   = errors.New("docker executable identity validation failed")
)

type Connector struct{}

func New() Connector {
	return Connector{}
}

func (Connector) Kind() string {
	return Kind
}

func (Connector) Label() string {
	return Label
}

func (Connector) Version() string {
	return Version
}

func (Connector) ExecuteAction(ctx context.Context, runtime connectors.RuntimeContext, action connectors.PreparedAction) (connectors.ActionResult, error) {
	client, err := newDockerClient(runtime)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	if action.ActionName != ActionVersion {
		if _, _, err := client.verifyDockerIdentity(ctx); err != nil {
			return connectors.ActionResult{}, err
		}
	}
	switch action.ActionName {
	case ActionVersion:
		return executeVersion(ctx, client)
	case ActionListContainers:
		return executeListContainers(ctx, client, action.Payload)
	case ActionListImages:
		return executeListImages(ctx, client)
	case ActionListNetworks:
		return executeListNetworks(ctx, client)
	case ActionListVolumes:
		return executeListVolumes(ctx, client)
	case ActionInspectContainer:
		return executeInspectContainer(ctx, client, action.Payload)
	case ActionContainerLogs:
		return executeContainerLogs(ctx, client, action.Payload)
	case ActionContainerExec:
		return executeContainerExec(ctx, client, action.Payload)
	case ActionStartContainer:
		return executeContainerLifecycle(ctx, client, action.Payload, "start")
	case ActionStopContainer:
		return executeContainerLifecycle(ctx, client, action.Payload, "stop")
	case ActionRestartContainer:
		return executeContainerLifecycle(ctx, client, action.Payload, "restart")
	default:
		return connectors.ActionResult{}, ErrUnsupportedAction
	}
}

func (Connector) TestConnection(ctx context.Context, runtime connectors.RuntimeContext) (connectors.TestResult, error) {
	client, err := newDockerClient(runtime)
	if err != nil {
		return connectors.TestResult{Status: connectors.TestUnknownError, Message: err.Error()}, nil
	}
	_, result, err := client.verifyDockerIdentity(ctx)
	if err != nil {
		status := connectors.TestFailedNetwork
		if result.ExitCode != 0 {
			status = connectors.TestFailedPermission
		} else if errors.Is(err, errInvalidIdentity) {
			status = connectors.TestUnknownError
		}
		return connectors.TestResult{Status: status, Message: err.Error()}, nil
	}
	return connectors.TestResult{
		Status:  connectors.TestOK,
		Message: "Docker connection ok.",
		Details: map[string]any{
			"duration_ms": result.DurationMS,
			"mode":        connectionMode(runtime.Target),
		},
	}, nil
}
