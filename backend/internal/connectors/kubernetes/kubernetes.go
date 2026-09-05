// Package kubernetesconnector defines the Kubernetes connector contract.
package kubernetesconnector

import (
	"context"
	"errors"
	"regexp"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

const (
	Kind    = "kubernetes"
	Label   = "Kubernetes"
	Version = "0.2"

	ActionVersion        = "cluster_version"
	ActionListNamespaces = "list_namespaces"
	ActionListWorkloads  = "list_workloads"
	ActionListPods       = "list_pods"
	ActionListServices   = "list_services"
	ActionListIngress    = "list_ingress"
	ActionListNodes      = "list_nodes"
	ActionListEvents     = "list_events"
	ActionDescribe       = "describe_resource"
	ActionLogs           = "get_logs"
	ActionRolloutRestart = "rollout_restart"

	defaultKubectlCommand = "kubectl"
	defaultLogTail        = 200
	maxLogTail            = 2000
	maxKubectlBytes       = 1024 << 10
	maxLogBytes           = 512 << 10
	maxReasonBytes        = 2000
)

var (
	ErrUnsupportedAction = errors.New("unsupported kubernetes connector action")
	ErrMissingTransport  = errors.New("kubernetes connector command transport is unavailable")
	ErrInvalidConfig     = errors.New("kubernetes connector target config is invalid")
	ErrScopeDenied       = errors.New("kubernetes namespace is outside this credential profile scope")

	kubeNamePattern       = regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)
	kubectlCommandPattern = regexp.MustCompile(`^[A-Za-z0-9_./+-]+$`)
)

// Connector describes Kubernetes as a read-heavy connector over bounded kubectl
// templates. The MVP intentionally uses an SSH command transport and does not
// import kubeconfig/token material into AIPermission.
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
	client, err := newKubeClient(runtime)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	switch action.ActionName {
	case ActionVersion:
		return executeVersion(ctx, client)
	case ActionListNamespaces:
		return executeListNamespaces(ctx, client)
	case ActionListWorkloads:
		return executeListWorkloads(ctx, client, action.Payload)
	case ActionListPods:
		return executeListPods(ctx, client, action.Payload)
	case ActionListServices:
		return executeListServices(ctx, client, action.Payload)
	case ActionListIngress:
		return executeListIngress(ctx, client, action.Payload)
	case ActionListNodes:
		return executeListNodes(ctx, client)
	case ActionListEvents:
		return executeListEvents(ctx, client, action.Payload)
	case ActionDescribe:
		return executeDescribe(ctx, client, action.Payload)
	case ActionLogs:
		return executeLogs(ctx, client, action.Payload)
	case ActionRolloutRestart:
		return executeRolloutRestart(ctx, client, action.Payload)
	default:
		return connectors.ActionResult{}, ErrUnsupportedAction
	}
}

func (Connector) TestConnection(ctx context.Context, runtime connectors.RuntimeContext) (connectors.TestResult, error) {
	client, err := newKubeClient(runtime)
	if err != nil {
		return connectors.TestResult{Status: connectors.TestUnknownError, Message: err.Error()}, nil
	}
	result, err := client.run(ctx, client.baseCommand()+" get namespaces -o json", 20)
	if err != nil {
		return connectors.TestResult{Status: connectors.TestFailedNetwork, Message: err.Error()}, nil
	}
	if result.ExitCode != 0 {
		return connectors.TestResult{Status: connectors.TestFailedPermission, Message: kubeCommandError("kubectl get namespaces", result).Error()}, nil
	}
	return connectors.TestResult{
		Status:  connectors.TestOK,
		Message: "Kubernetes connection ok.",
		Details: map[string]any{
			"duration_ms": result.DurationMS,
			"mode":        connectionMode(runtime.Target),
		},
	}, nil
}
