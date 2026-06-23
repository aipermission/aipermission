// Package kubernetesconnector defines the Kubernetes connector contract.
package kubernetesconnector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

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

	kubeNamePattern = regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)
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

func (Connector) TargetSchema() connectors.Schema {
	return connectors.Schema{Fields: []connectors.Field{
		{
			Name:        "connection_mode",
			Label:       "Connection mode",
			Type:        connectors.FieldSelect,
			Required:    true,
			Default:     "over_ssh",
			Description: "Run bounded kubectl templates through an SSH connector profile.",
			Options: []connectors.FieldOption{
				{Value: "over_ssh", Label: "Over SSH"},
			},
		},
		{
			Name:        "transport_target_ref",
			Label:       "SSH transport profile",
			Type:        connectors.FieldString,
			Required:    true,
			Description: "SSH connector target/profile ref used to run kubectl.",
		},
		{
			Name:        "kubectl_command",
			Label:       "kubectl command",
			Type:        connectors.FieldString,
			Default:     defaultKubectlCommand,
			Description: "kubectl command on the remote host. Keep this as kubectl unless the host uses a wrapper path.",
		},
		{
			Name:        "context",
			Label:       "Context",
			Type:        connectors.FieldString,
			Description: "Optional kubectl context name.",
		},
		{
			Name:        "default_namespace",
			Label:       "Default namespace",
			Type:        connectors.FieldString,
			Description: "Optional default namespace for actions that need one.",
		},
	}}
}

func (Connector) CredentialSchemas() []connectors.CredentialSchema {
	return []connectors.CredentialSchema{
		{
			Kind:        "namespace_scope",
			Label:       "Namespace scope",
			Description: "Restrict this profile to all namespaces or to selected namespaces.",
			Schema: connectors.Schema{Fields: []connectors.Field{
				{
					Name:        "scope_mode",
					Label:       "Scope",
					Type:        connectors.FieldSelect,
					Required:    true,
					Default:     "all",
					Description: "Use selected when this token should only see and operate on specific namespaces.",
					Options: []connectors.FieldOption{
						{Value: "all", Label: "All namespaces"},
						{Value: "selected", Label: "Selected namespaces"},
					},
				},
				{
					Name:        "namespaces",
					Label:       "Namespaces",
					Type:        connectors.FieldMultiline,
					Description: "One namespace per line. Required when scope is selected.",
				},
			}},
		},
	}
}

func (Connector) GetHelp(_ context.Context, target connectors.TargetView) (connectors.ConnectorHelp, error) {
	title := "Kubernetes target"
	if strings.TrimSpace(target.Name) != "" {
		title = "Kubernetes target: " + target.Name
	}
	return connectors.ConnectorHelp{
		Title:       title,
		Summary:     "Inspect Kubernetes namespaces, workloads, pods, services, events, nodes, and bounded logs through kubectl templates and AIPermission approval rules.",
		Connector:   Label,
		ConnectorID: Kind,
		Usage: []string{
			"Use list_namespaces first when the namespace is unknown.",
			"Use list_workloads and list_pods to identify unhealthy deployments or pods.",
			"Use list_events to find scheduling, image pull, probe, and crash loop clues.",
			"Use get_logs with a bounded tail value for recent pod logs.",
			"Use rollout_restart only when the operator intends a deployment restart.",
		},
		Warnings: []string{
			"Kubernetes actions run through a selected SSH transport profile. The connector does not expose raw kubectl, apply, delete, pod exec, or Secret value browsing.",
			"Logs and resource JSON may contain sensitive application data. Redaction is best-effort; avoid requesting sensitive logs unless approved.",
			"Credential profile namespace scope can restrict AI access to a selected set of namespaces.",
		},
	}, nil
}

func (Connector) GetActionList(context.Context, connectors.TargetView, connectors.CredentialProfileView) ([]connectors.ActionDefinition, error) {
	return []connectors.ActionDefinition{
		{Name: ActionVersion, Label: "Cluster version", Description: "Read Kubernetes client/server version metadata.", Category: "metadata", Risk: connectors.RiskRead, InputSchema: connectors.Schema{}, OutputHint: connectors.OutputHint{Format: "json", MaxBytes: 64 << 10}},
		{Name: ActionListNamespaces, Label: "List namespaces", Description: "List Kubernetes namespaces visible to this profile.", Category: "browser", Risk: connectors.RiskRead, InputSchema: connectors.Schema{}, OutputHint: connectors.OutputHint{Format: "json", MaxRows: 500}},
		{Name: ActionListWorkloads, Label: "List workloads", Description: "List deployments, statefulsets, and daemonsets.", Category: "browser", Risk: connectors.RiskRead, InputSchema: namespaceInputSchema(), OutputHint: connectors.OutputHint{Format: "json", MaxRows: 1000}},
		{Name: ActionListPods, Label: "List pods", Description: "List pods with status, readiness, restarts, node, and age metadata.", Category: "browser", Risk: connectors.RiskRead, InputSchema: namespaceInputSchema(), OutputHint: connectors.OutputHint{Format: "json", MaxRows: 2000}},
		{Name: ActionListServices, Label: "List services", Description: "List services and exposed ports.", Category: "browser", Risk: connectors.RiskRead, InputSchema: namespaceInputSchema(), OutputHint: connectors.OutputHint{Format: "json", MaxRows: 1000}},
		{Name: ActionListIngress, Label: "List ingress", Description: "List ingress hosts and paths where available.", Category: "browser", Risk: connectors.RiskRead, InputSchema: namespaceInputSchema(), OutputHint: connectors.OutputHint{Format: "json", MaxRows: 1000}},
		{Name: ActionListNodes, Label: "List nodes", Description: "List cluster nodes, versions, roles, and readiness.", Category: "browser", Risk: connectors.RiskRead, InputSchema: connectors.Schema{}, OutputHint: connectors.OutputHint{Format: "json", MaxRows: 500}},
		{Name: ActionListEvents, Label: "List events", Description: "List warning-first Kubernetes events.", Category: "browser", Risk: connectors.RiskRead, InputSchema: connectors.Schema{Fields: []connectors.Field{{Name: "namespace", Label: "Namespace", Type: connectors.FieldString, Description: "Optional namespace. Empty lists across allowed namespaces."}, {Name: "limit", Label: "Limit", Type: connectors.FieldNumber, Default: 200}}}, OutputHint: connectors.OutputHint{Format: "json", MaxRows: 1000}},
		{Name: ActionDescribe, Label: "Describe resource", Description: "Read JSON metadata for one Kubernetes resource.", Category: "browser", Risk: connectors.RiskRead, InputSchema: connectors.Schema{Fields: []connectors.Field{{Name: "resource_type", Label: "Resource type", Type: connectors.FieldSelect, Required: true, Options: []connectors.FieldOption{{Value: "pod", Label: "Pod"}, {Value: "deployment", Label: "Deployment"}, {Value: "statefulset", Label: "StatefulSet"}, {Value: "daemonset", Label: "DaemonSet"}, {Value: "service", Label: "Service"}, {Value: "ingress", Label: "Ingress"}, {Value: "node", Label: "Node"}}}, {Name: "name", Label: "Name", Type: connectors.FieldString, Required: true}, {Name: "namespace", Label: "Namespace", Type: connectors.FieldString, Description: "Required for namespaced resources unless target default namespace is set."}}}, OutputHint: connectors.OutputHint{Format: "json", MaxBytes: maxKubectlBytes}},
		{Name: ActionLogs, Label: "Pod logs", Description: "Read a bounded tail of pod logs.", Category: "browser", Risk: connectors.RiskRead, InputSchema: connectors.Schema{Fields: []connectors.Field{{Name: "namespace", Label: "Namespace", Type: connectors.FieldString, Required: true}, {Name: "pod", Label: "Pod", Type: connectors.FieldString, Required: true}, {Name: "container", Label: "Container", Type: connectors.FieldString, Description: "Optional container name."}, {Name: "tail", Label: "Tail lines", Type: connectors.FieldNumber, Default: defaultLogTail}}}, OutputHint: connectors.OutputHint{Format: "text", MaxBytes: maxLogBytes}},
		{Name: ActionRolloutRestart, Label: "Rollout restart", Description: "Restart one Kubernetes deployment.", Category: "lifecycle", Risk: connectors.RiskWrite, InputSchema: connectors.Schema{Fields: []connectors.Field{{Name: "namespace", Label: "Namespace", Type: connectors.FieldString, Required: true}, {Name: "deployment", Label: "Deployment", Type: connectors.FieldString, Required: true}}}, OutputHint: connectors.OutputHint{Format: "json", MaxBytes: 4000}},
	}, nil
}

func namespaceInputSchema() connectors.Schema {
	return connectors.Schema{Fields: []connectors.Field{{Name: "namespace", Label: "Namespace", Type: connectors.FieldString, Description: "Optional namespace. Empty lists across allowed namespaces."}}}
}

func (Connector) PrepareAction(_ context.Context, req connectors.ActionRequest) (connectors.PreparedAction, error) {
	if len(req.Reason) > maxReasonBytes {
		return connectors.PreparedAction{}, fmt.Errorf("reason is too large")
	}
	input := copyMap(req.Input)
	risk := connectors.RiskRead
	title := ""
	summary := ""
	switch req.ActionName {
	case ActionVersion:
		title = "Read Kubernetes version"
		summary = "Read client/server version metadata."
	case ActionListNamespaces:
		title = "List Kubernetes namespaces"
		summary = "List namespaces visible to this profile."
	case ActionListWorkloads:
		input["namespace"] = normalizeOptionalName(input, "namespace")
		title = "List Kubernetes workloads"
		summary = namespaceSummary(input)
	case ActionListPods:
		input["namespace"] = normalizeOptionalName(input, "namespace")
		title = "List Kubernetes pods"
		summary = namespaceSummary(input)
	case ActionListServices:
		input["namespace"] = normalizeOptionalName(input, "namespace")
		title = "List Kubernetes services"
		summary = namespaceSummary(input)
	case ActionListIngress:
		input["namespace"] = normalizeOptionalName(input, "namespace")
		title = "List Kubernetes ingress"
		summary = namespaceSummary(input)
	case ActionListNodes:
		title = "List Kubernetes nodes"
		summary = "List cluster nodes."
	case ActionListEvents:
		input["namespace"] = normalizeOptionalName(input, "namespace")
		input["limit"] = normalizeInt(input, "limit", 200, 1, 1000)
		title = "List Kubernetes events"
		summary = namespaceSummary(input)
	case ActionDescribe:
		resourceType := normalizeResourceType(input)
		if resourceType == "" {
			return connectors.PreparedAction{}, fmt.Errorf("resource_type is required")
		}
		name := normalizeRequiredName(input, "name")
		if name == "" {
			return connectors.PreparedAction{}, fmt.Errorf("name is required")
		}
		input["resource_type"] = resourceType
		input["name"] = name
		input["namespace"] = normalizeOptionalName(input, "namespace")
		title = "Describe Kubernetes resource"
		summary = resourceSummary(resourceType, stringValue(input, "namespace"), name)
	case ActionLogs:
		namespace := normalizeRequiredName(input, "namespace")
		pod := normalizeRequiredName(input, "pod")
		if namespace == "" || pod == "" {
			return connectors.PreparedAction{}, fmt.Errorf("namespace and pod are required")
		}
		input["namespace"] = namespace
		input["pod"] = pod
		input["container"] = normalizeOptionalName(input, "container")
		input["tail"] = normalizeInt(input, "tail", defaultLogTail, 1, maxLogTail)
		title = "Read Kubernetes pod logs"
		summary = fmt.Sprintf("%s/%s tail=%d", namespace, pod, input["tail"])
	case ActionRolloutRestart:
		risk = connectors.RiskWrite
		namespace := normalizeRequiredName(input, "namespace")
		deployment := normalizeRequiredName(input, "deployment")
		if namespace == "" || deployment == "" {
			return connectors.PreparedAction{}, fmt.Errorf("namespace and deployment are required")
		}
		input["namespace"] = namespace
		input["deployment"] = deployment
		title = "Rollout restart Kubernetes deployment"
		summary = fmt.Sprintf("%s/%s", namespace, deployment)
	default:
		return connectors.PreparedAction{}, ErrUnsupportedAction
	}
	return connectors.PreparedAction{
		ConnectorKind: Kind,
		TargetRef:     req.Target.Ref,
		ProfileID:     req.Profile.ID,
		ActionName:    req.ActionName,
		Risk:          risk,
		Title:         title,
		Summary:       summary,
		Preview:       input,
		Payload:       input,
		ContextMaterial: map[string]any{
			"target":          req.Target.Name,
			"profile":         req.Profile.Label,
			"connection_mode": connectionMode(req.Target),
			"scope_mode":      scopeMode(req.Profile),
		},
	}, nil
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

type kubeClient struct {
	runtime   connectors.RuntimeContext
	transport connectors.CommandTransport
	command   string
	context   string
	scope     kubeScope
}

func newKubeClient(runtime connectors.RuntimeContext) (*kubeClient, error) {
	transport, _ := runtime.Capability(connectors.CommandTransportCapabilityName).(connectors.CommandTransport)
	if transport == nil {
		return nil, ErrMissingTransport
	}
	command := strings.TrimSpace(stringValue(runtime.Target.Config, "kubectl_command"))
	if command == "" {
		command = defaultKubectlCommand
	}
	return &kubeClient{
		runtime:   runtime,
		transport: transport,
		command:   command,
		context:   strings.TrimSpace(stringValue(runtime.Target.Config, "context")),
		scope:     kubeScopeFromProfile(runtime.Profile),
	}, nil
}

func (client *kubeClient) baseCommand() string {
	base := client.command
	if client.context != "" {
		base += " --context " + shellQuote(client.context)
	}
	return base
}

func (client *kubeClient) run(ctx context.Context, command string, timeoutSeconds int) (connectors.CommandRunResult, error) {
	return client.transport.RunConnectorCommand(ctx, connectors.CommandRunRequest{
		Mode:               connectionMode(client.runtime.Target),
		TransportTargetRef: strings.TrimSpace(stringValue(client.runtime.Target.Config, "transport_target_ref")),
		Command:            command,
		TimeoutSeconds:     timeoutSeconds,
	})
}

func executeVersion(ctx context.Context, client *kubeClient) (connectors.ActionResult, error) {
	result, err := client.run(ctx, client.baseCommand()+" version -o json", 20)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	if result.ExitCode != 0 {
		return connectors.ActionResult{}, kubeCommandError("kubectl version", result)
	}
	var output any
	if err := json.Unmarshal([]byte(result.Stdout), &output); err != nil {
		output = map[string]any{"raw": truncateString(result.Stdout, maxKubectlBytes)}
	}
	return connectors.ActionResult{Status: connectors.ResultCompleted, Output: map[string]any{"version": output, "duration_ms": result.DurationMS}, DisplayText: truncateString(result.Stdout, 4000)}, nil
}

func executeListNamespaces(ctx context.Context, client *kubeClient) (connectors.ActionResult, error) {
	items, err := client.runKubeList(ctx, client.baseCommand()+" get namespaces -o json", 20)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	namespaces := make([]NamespaceSummary, 0, len(items))
	for _, item := range items {
		summary := namespaceSummaryFromItem(item)
		if client.scope.namespaceAllowed(summary.Name) {
			namespaces = append(namespaces, summary)
		}
	}
	sort.SliceStable(namespaces, func(i, j int) bool { return namespaces[i].Name < namespaces[j].Name })
	return connectors.ActionResult{Status: connectors.ResultCompleted, Output: map[string]any{"namespaces": namespaces, "count": len(namespaces), "scope_mode": client.scope.mode}, DisplayText: fmt.Sprintf("Listed %d Kubernetes namespace(s).", len(namespaces))}, nil
}

func executeListWorkloads(ctx context.Context, client *kubeClient, input map[string]any) (connectors.ActionResult, error) {
	items, err := client.runNamespacedKubeList(ctx, "deployments,statefulsets,daemonsets", stringValue(input, "namespace"), 25)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	workloads := make([]WorkloadSummary, 0, len(items))
	for _, item := range items {
		workloads = append(workloads, workloadSummaryFromItem(item))
	}
	sort.SliceStable(workloads, func(i, j int) bool {
		if workloads[i].Namespace == workloads[j].Namespace {
			return workloads[i].Name < workloads[j].Name
		}
		return workloads[i].Namespace < workloads[j].Namespace
	})
	return connectors.ActionResult{Status: connectors.ResultCompleted, Output: map[string]any{"workloads": workloads, "count": len(workloads)}, DisplayText: workloadsDisplay(workloads)}, nil
}

func executeListPods(ctx context.Context, client *kubeClient, input map[string]any) (connectors.ActionResult, error) {
	items, err := client.runNamespacedKubeList(ctx, "pods", stringValue(input, "namespace"), 25)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	pods := make([]PodSummary, 0, len(items))
	for _, item := range items {
		pods = append(pods, podSummaryFromItem(item))
	}
	sort.SliceStable(pods, func(i, j int) bool {
		if pods[i].Namespace == pods[j].Namespace {
			return pods[i].Name < pods[j].Name
		}
		return pods[i].Namespace < pods[j].Namespace
	})
	return connectors.ActionResult{Status: connectors.ResultCompleted, Output: map[string]any{"pods": pods, "count": len(pods)}, DisplayText: podsDisplay(pods)}, nil
}

func executeListServices(ctx context.Context, client *kubeClient, input map[string]any) (connectors.ActionResult, error) {
	items, err := client.runNamespacedKubeList(ctx, "services", stringValue(input, "namespace"), 25)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	services := make([]ServiceSummary, 0, len(items))
	for _, item := range items {
		services = append(services, serviceSummaryFromItem(item))
	}
	return connectors.ActionResult{Status: connectors.ResultCompleted, Output: map[string]any{"services": services, "count": len(services)}, DisplayText: fmt.Sprintf("Listed %d Kubernetes service(s).", len(services))}, nil
}

func executeListIngress(ctx context.Context, client *kubeClient, input map[string]any) (connectors.ActionResult, error) {
	items, err := client.runNamespacedKubeList(ctx, "ingress", stringValue(input, "namespace"), 25)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	ingresses := make([]IngressSummary, 0, len(items))
	for _, item := range items {
		ingresses = append(ingresses, ingressSummaryFromItem(item))
	}
	return connectors.ActionResult{Status: connectors.ResultCompleted, Output: map[string]any{"ingress": ingresses, "count": len(ingresses)}, DisplayText: fmt.Sprintf("Listed %d Kubernetes ingress resource(s).", len(ingresses))}, nil
}

func executeListNodes(ctx context.Context, client *kubeClient) (connectors.ActionResult, error) {
	items, err := client.runKubeList(ctx, client.baseCommand()+" get nodes -o json", 25)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	nodes := make([]NodeSummary, 0, len(items))
	for _, item := range items {
		nodes = append(nodes, nodeSummaryFromItem(item))
	}
	return connectors.ActionResult{Status: connectors.ResultCompleted, Output: map[string]any{"nodes": nodes, "count": len(nodes)}, DisplayText: fmt.Sprintf("Listed %d Kubernetes node(s).", len(nodes))}, nil
}

func executeListEvents(ctx context.Context, client *kubeClient, input map[string]any) (connectors.ActionResult, error) {
	limit := normalizeInt(input, "limit", 200, 1, 1000)
	items, err := client.runNamespacedKubeList(ctx, "events", stringValue(input, "namespace"), 25)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	events := make([]EventSummary, 0, len(items))
	for _, item := range items {
		events = append(events, eventSummaryFromItem(item))
	}
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].Type == "Warning" && events[j].Type != "Warning" {
			return true
		}
		if events[j].Type == "Warning" && events[i].Type != "Warning" {
			return false
		}
		return events[i].LastTimestamp > events[j].LastTimestamp
	})
	if len(events) > limit {
		events = events[:limit]
	}
	return connectors.ActionResult{Status: connectors.ResultCompleted, Output: map[string]any{"events": events, "count": len(events)}, DisplayText: fmt.Sprintf("Listed %d Kubernetes event(s).", len(events))}, nil
}

func executeDescribe(ctx context.Context, client *kubeClient, input map[string]any) (connectors.ActionResult, error) {
	resourceType := normalizeResourceType(input)
	name := normalizeRequiredName(input, "name")
	namespace := namespaceOrDefault(client.runtime.Target, stringValue(input, "namespace"))
	if resourceType != "node" {
		if namespace == "" {
			return connectors.ActionResult{}, fmt.Errorf("namespace is required for %s", resourceType)
		}
		if err := client.scope.ensureNamespace(namespace); err != nil {
			return connectors.ActionResult{}, err
		}
	}
	command := fmt.Sprintf("%s get %s %s -o json", client.baseCommand(), resourceType, shellQuote(name))
	if resourceType != "node" {
		command = fmt.Sprintf("%s get %s %s -n %s -o json", client.baseCommand(), resourceType, shellQuote(name), shellQuote(namespace))
	}
	result, err := client.run(ctx, command, 25)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	if result.ExitCode != 0 {
		return connectors.ActionResult{}, kubeCommandError("kubectl get "+resourceType, result)
	}
	var resource map[string]any
	if err := json.Unmarshal([]byte(result.Stdout), &resource); err != nil {
		return connectors.ActionResult{}, fmt.Errorf("parse kubectl resource JSON: %w", err)
	}
	return connectors.ActionResult{Status: connectors.ResultCompleted, Output: map[string]any{"resource": resource, "summary": genericResourceSummary(resource)}, DisplayText: fmt.Sprintf("Read Kubernetes %s %s.", resourceType, resourceSummary(resourceType, namespace, name))}, nil
}

func executeLogs(ctx context.Context, client *kubeClient, input map[string]any) (connectors.ActionResult, error) {
	namespace := normalizeRequiredName(input, "namespace")
	pod := normalizeRequiredName(input, "pod")
	container := normalizeOptionalName(input, "container")
	if namespace == "" || pod == "" {
		return connectors.ActionResult{}, fmt.Errorf("namespace and pod are required")
	}
	if err := client.scope.ensureNamespace(namespace); err != nil {
		return connectors.ActionResult{}, err
	}
	tail := normalizeInt(input, "tail", defaultLogTail, 1, maxLogTail)
	command := fmt.Sprintf("%s logs -n %s %s --tail %d --timestamps=true", client.baseCommand(), shellQuote(namespace), shellQuote(pod), tail)
	if container != "" {
		command += " -c " + shellQuote(container)
	}
	command += " 2>&1"
	result, err := client.run(ctx, command, 35)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	if result.ExitCode != 0 {
		return connectors.ActionResult{}, kubeCommandError("kubectl logs", result)
	}
	logs := truncateString(result.Stdout, maxLogBytes)
	return connectors.ActionResult{Status: connectors.ResultCompleted, Output: map[string]any{"namespace": namespace, "pod": pod, "container": container, "tail": tail, "logs": logs, "duration_ms": result.DurationMS}, DisplayText: logs}, nil
}

func executeRolloutRestart(ctx context.Context, client *kubeClient, input map[string]any) (connectors.ActionResult, error) {
	namespace := normalizeRequiredName(input, "namespace")
	deployment := normalizeRequiredName(input, "deployment")
	if namespace == "" || deployment == "" {
		return connectors.ActionResult{}, fmt.Errorf("namespace and deployment are required")
	}
	if err := client.scope.ensureNamespace(namespace); err != nil {
		return connectors.ActionResult{}, err
	}
	command := fmt.Sprintf("%s rollout restart deployment/%s -n %s 2>&1", client.baseCommand(), shellQuote(deployment), shellQuote(namespace))
	result, err := client.run(ctx, command, 30)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	if result.ExitCode != 0 {
		return connectors.ActionResult{}, kubeCommandError("kubectl rollout restart", result)
	}
	return connectors.ActionResult{Status: connectors.ResultCompleted, Output: map[string]any{"namespace": namespace, "deployment": deployment, "response": strings.TrimSpace(result.Stdout), "duration_ms": result.DurationMS}, DisplayText: strings.TrimSpace(result.Stdout)}, nil
}

func (client *kubeClient) runKubeList(ctx context.Context, command string, timeoutSeconds int) ([]map[string]any, error) {
	result, err := client.run(ctx, command, timeoutSeconds)
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		return nil, kubeCommandError("kubectl get", result)
	}
	if len(result.Stdout) > maxKubectlBytes {
		return nil, fmt.Errorf("kubectl output is larger than %d bytes", maxKubectlBytes)
	}
	var list struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &list); err != nil {
		return nil, fmt.Errorf("parse kubectl JSON list: %w", err)
	}
	return list.Items, nil
}

func (client *kubeClient) runNamespacedKubeList(ctx context.Context, resource string, namespace string, timeoutSeconds int) ([]map[string]any, error) {
	namespaces, clusterWide, err := client.namespacesForQuery(namespace)
	if err != nil {
		return nil, err
	}
	if clusterWide {
		return client.runKubeList(ctx, fmt.Sprintf("%s get %s -A -o json", client.baseCommand(), resource), timeoutSeconds)
	}
	var all []map[string]any
	for _, ns := range namespaces {
		items, err := client.runKubeList(ctx, fmt.Sprintf("%s get %s -n %s -o json", client.baseCommand(), resource, shellQuote(ns)), timeoutSeconds)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
	}
	return all, nil
}

// ProfileAllowsNamespace reports whether a public credential profile can access
// one namespace. Runtime adapters use this for connector-owned live-console
// surfaces such as a pod shell.
func ProfileAllowsNamespace(profile connectors.CredentialProfileView, namespace string) bool {
	return kubeScopeFromProfile(profile).namespaceAllowed(strings.TrimSpace(namespace))
}

func (client *kubeClient) namespacesForQuery(namespace string) ([]string, bool, error) {
	namespace = namespaceOrDefault(client.runtime.Target, namespace)
	if namespace != "" {
		if err := client.scope.ensureNamespace(namespace); err != nil {
			return nil, false, err
		}
		return []string{namespace}, false, nil
	}
	if client.scope.mode == "selected" {
		if len(client.scope.namespaces) == 0 {
			return nil, false, fmt.Errorf("%w: selected scope has no namespaces", ErrInvalidConfig)
		}
		return append([]string(nil), client.scope.namespaces...), false, nil
	}
	return nil, true, nil
}

type kubeScope struct {
	mode       string
	namespaces []string
	allowed    map[string]bool
}

func kubeScopeFromProfile(profile connectors.CredentialProfileView) kubeScope {
	mode := strings.TrimSpace(stringValue(profile.Public, "scope_mode"))
	if mode == "" {
		mode = "all"
	}
	namespaces := splitLines(stringValue(profile.Public, "namespaces"))
	scope := kubeScope{mode: mode, namespaces: namespaces, allowed: map[string]bool{}}
	for _, namespace := range namespaces {
		scope.allowed[namespace] = true
	}
	return scope
}

func (scope kubeScope) namespaceAllowed(namespace string) bool {
	if scope.mode != "selected" {
		return true
	}
	return scope.allowed[namespace]
}

func (scope kubeScope) ensureNamespace(namespace string) error {
	if namespace == "" || !validKubeName(namespace) {
		return fmt.Errorf("invalid namespace")
	}
	if !scope.namespaceAllowed(namespace) {
		return fmt.Errorf("%w: %s", ErrScopeDenied, namespace)
	}
	return nil
}

type NamespaceSummary struct {
	Name      string `json:"name"`
	Status    string `json:"status,omitempty"`
	Age       string `json:"age,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
}

type WorkloadSummary struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Ready     string `json:"ready,omitempty"`
	Replicas  int    `json:"replicas"`
	Available int    `json:"available"`
	Image     string `json:"image,omitempty"`
	Age       string `json:"age,omitempty"`
}

type PodSummary struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Phase     string `json:"phase,omitempty"`
	Ready     string `json:"ready,omitempty"`
	Restarts  int    `json:"restarts"`
	Node      string `json:"node,omitempty"`
	Age       string `json:"age,omitempty"`
}

type ServiceSummary struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Type      string `json:"type,omitempty"`
	ClusterIP string `json:"cluster_ip,omitempty"`
	Ports     string `json:"ports,omitempty"`
	Age       string `json:"age,omitempty"`
}

type IngressSummary struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Class     string `json:"class,omitempty"`
	Hosts     string `json:"hosts,omitempty"`
	Age       string `json:"age,omitempty"`
}

type NodeSummary struct {
	Name    string `json:"name"`
	Ready   string `json:"ready,omitempty"`
	Roles   string `json:"roles,omitempty"`
	Version string `json:"version,omitempty"`
	Age     string `json:"age,omitempty"`
}

type EventSummary struct {
	Namespace     string `json:"namespace"`
	Type          string `json:"type,omitempty"`
	Reason        string `json:"reason,omitempty"`
	Object        string `json:"object,omitempty"`
	Message       string `json:"message,omitempty"`
	LastTimestamp string `json:"last_timestamp,omitempty"`
	Count         int    `json:"count,omitempty"`
}

func namespaceSummaryFromItem(item map[string]any) NamespaceSummary {
	metadata := mapValue(item, "metadata")
	status := mapValue(item, "status")
	return NamespaceSummary{Name: stringValue(metadata, "name"), Status: stringValue(status, "phase"), CreatedAt: stringValue(metadata, "creationTimestamp"), Age: ageText(stringValue(metadata, "creationTimestamp"))}
}

func workloadSummaryFromItem(item map[string]any) WorkloadSummary {
	metadata := mapValue(item, "metadata")
	spec := mapValue(item, "spec")
	status := mapValue(item, "status")
	return WorkloadSummary{
		Kind:      stringValue(item, "kind"),
		Namespace: stringValue(metadata, "namespace"),
		Name:      stringValue(metadata, "name"),
		Ready:     fmt.Sprintf("%d/%d", intValue(status, "readyReplicas"), intValue(spec, "replicas")),
		Replicas:  intValue(spec, "replicas"),
		Available: intValue(status, "availableReplicas"),
		Image:     firstContainerImage(item),
		Age:       ageText(stringValue(metadata, "creationTimestamp")),
	}
}

func podSummaryFromItem(item map[string]any) PodSummary {
	metadata := mapValue(item, "metadata")
	status := mapValue(item, "status")
	ready, restarts := podReadyRestarts(status)
	return PodSummary{Namespace: stringValue(metadata, "namespace"), Name: stringValue(metadata, "name"), Phase: stringValue(status, "phase"), Ready: ready, Restarts: restarts, Node: stringValue(mapValue(item, "spec"), "nodeName"), Age: ageText(stringValue(metadata, "creationTimestamp"))}
}

func serviceSummaryFromItem(item map[string]any) ServiceSummary {
	metadata := mapValue(item, "metadata")
	spec := mapValue(item, "spec")
	return ServiceSummary{Namespace: stringValue(metadata, "namespace"), Name: stringValue(metadata, "name"), Type: stringValue(spec, "type"), ClusterIP: stringValue(spec, "clusterIP"), Ports: servicePorts(spec), Age: ageText(stringValue(metadata, "creationTimestamp"))}
}

func ingressSummaryFromItem(item map[string]any) IngressSummary {
	metadata := mapValue(item, "metadata")
	spec := mapValue(item, "spec")
	return IngressSummary{Namespace: stringValue(metadata, "namespace"), Name: stringValue(metadata, "name"), Class: stringValue(spec, "ingressClassName"), Hosts: ingressHosts(spec), Age: ageText(stringValue(metadata, "creationTimestamp"))}
}

func nodeSummaryFromItem(item map[string]any) NodeSummary {
	metadata := mapValue(item, "metadata")
	status := mapValue(item, "status")
	info := mapValue(status, "nodeInfo")
	return NodeSummary{Name: stringValue(metadata, "name"), Ready: nodeReady(status), Roles: nodeRoles(metadata), Version: stringValue(info, "kubeletVersion"), Age: ageText(stringValue(metadata, "creationTimestamp"))}
}

func eventSummaryFromItem(item map[string]any) EventSummary {
	metadata := mapValue(item, "metadata")
	involved := mapValue(item, "involvedObject")
	last := stringValue(item, "lastTimestamp")
	if last == "" {
		last = stringValue(item, "eventTime")
	}
	return EventSummary{Namespace: stringValue(metadata, "namespace"), Type: stringValue(item, "type"), Reason: stringValue(item, "reason"), Object: strings.Trim(strings.Join([]string{stringValue(involved, "kind"), stringValue(involved, "name")}, "/"), "/"), Message: truncateString(stringValue(item, "message"), 2000), LastTimestamp: last, Count: intValue(item, "count")}
}

func genericResourceSummary(resource map[string]any) map[string]any {
	metadata := mapValue(resource, "metadata")
	return map[string]any{"kind": stringValue(resource, "kind"), "namespace": stringValue(metadata, "namespace"), "name": stringValue(metadata, "name"), "created_at": stringValue(metadata, "creationTimestamp")}
}

func firstContainerImage(item map[string]any) string {
	template := mapValue(mapValue(mapValue(item, "spec"), "template"), "spec")
	containers := sliceValue(template, "containers")
	if len(containers) == 0 {
		return ""
	}
	return stringValue(containers[0], "image")
}

func podReadyRestarts(status map[string]any) (string, int) {
	statuses := sliceValue(status, "containerStatuses")
	ready := 0
	restarts := 0
	for _, container := range statuses {
		if boolValue(container, "ready") {
			ready++
		}
		restarts += intValue(container, "restartCount")
	}
	return fmt.Sprintf("%d/%d", ready, len(statuses)), restarts
}

func servicePorts(spec map[string]any) string {
	var parts []string
	for _, port := range sliceValue(spec, "ports") {
		text := fmt.Sprintf("%d/%s", intValue(port, "port"), strings.ToUpper(stringValue(port, "protocol")))
		if nodePort := intValue(port, "nodePort"); nodePort > 0 {
			text += fmt.Sprintf(":%d", nodePort)
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, ", ")
}

func ingressHosts(spec map[string]any) string {
	var hosts []string
	for _, rule := range sliceValue(spec, "rules") {
		if host := stringValue(rule, "host"); host != "" {
			hosts = append(hosts, host)
		}
	}
	return strings.Join(hosts, ", ")
}

func nodeReady(status map[string]any) string {
	for _, condition := range sliceValue(status, "conditions") {
		if stringValue(condition, "type") == "Ready" {
			return stringValue(condition, "status")
		}
	}
	return ""
}

func nodeRoles(metadata map[string]any) string {
	labels := mapValue(metadata, "labels")
	var roles []string
	for key := range labels {
		if strings.HasPrefix(key, "node-role.kubernetes.io/") {
			role := strings.TrimPrefix(key, "node-role.kubernetes.io/")
			if role == "" {
				role = "control-plane"
			}
			roles = append(roles, role)
		}
	}
	sort.Strings(roles)
	if len(roles) == 0 {
		return "worker"
	}
	return strings.Join(roles, ",")
}

func workloadsDisplay(workloads []WorkloadSummary) string {
	if len(workloads) == 0 {
		return "No Kubernetes workloads found."
	}
	lines := make([]string, 0, len(workloads))
	for _, workload := range workloads {
		lines = append(lines, fmt.Sprintf("%s/%s/%s ready=%s image=%s", workload.Namespace, workload.Kind, workload.Name, workload.Ready, workload.Image))
	}
	return strings.Join(lines, "\n")
}

func podsDisplay(pods []PodSummary) string {
	if len(pods) == 0 {
		return "No Kubernetes pods found."
	}
	lines := make([]string, 0, len(pods))
	for _, pod := range pods {
		lines = append(lines, fmt.Sprintf("%s/%s phase=%s ready=%s restarts=%d", pod.Namespace, pod.Name, pod.Phase, pod.Ready, pod.Restarts))
	}
	return strings.Join(lines, "\n")
}

func kubeCommandError(command string, result connectors.CommandRunResult) error {
	text := strings.TrimSpace(result.Stderr)
	if text == "" {
		text = strings.TrimSpace(result.Stdout)
	}
	if text == "" {
		text = fmt.Sprintf("exit code %d", result.ExitCode)
	}
	return fmt.Errorf("%s failed: %s", command, truncateString(text, 2000))
}

func connectionMode(target connectors.TargetView) string {
	mode := strings.TrimSpace(stringValue(target.Config, "connection_mode"))
	if mode == "" {
		return "over_ssh"
	}
	return mode
}

func scopeMode(profile connectors.CredentialProfileView) string {
	mode := strings.TrimSpace(stringValue(profile.Public, "scope_mode"))
	if mode == "" {
		return "all"
	}
	return mode
}

func namespaceOrDefault(target connectors.TargetView, namespace string) string {
	namespace = strings.TrimSpace(namespace)
	if namespace != "" {
		return namespace
	}
	return strings.TrimSpace(stringValue(target.Config, "default_namespace"))
}

func normalizeResourceType(input map[string]any) string {
	value := strings.ToLower(strings.TrimSpace(stringValue(input, "resource_type")))
	switch value {
	case "pod", "deployment", "statefulset", "daemonset", "service", "ingress", "node":
		return value
	default:
		return ""
	}
}

func normalizeRequiredName(input map[string]any, key string) string {
	value := strings.TrimSpace(stringValue(input, key))
	if !validKubeName(value) {
		return ""
	}
	return value
}

func normalizeOptionalName(input map[string]any, key string) string {
	value := strings.TrimSpace(stringValue(input, key))
	if value == "" {
		return ""
	}
	if !validKubeName(value) {
		return ""
	}
	return value
}

func validKubeName(value string) bool {
	return value != "" && kubeNamePattern.MatchString(value)
}

func resourceSummary(resourceType string, namespace string, name string) string {
	if namespace == "" {
		return resourceType + "/" + name
	}
	return namespace + "/" + resourceType + "/" + name
}

func namespaceSummary(input map[string]any) string {
	if namespace := stringValue(input, "namespace"); namespace != "" {
		return "namespace " + namespace
	}
	return "allowed namespaces"
}

func mapValue(value any, key string) map[string]any {
	source, ok := value.(map[string]any)
	if !ok || source == nil {
		return map[string]any{}
	}
	child, ok := source[key].(map[string]any)
	if !ok || child == nil {
		return map[string]any{}
	}
	return child
}

func sliceValue(source map[string]any, key string) []map[string]any {
	values, ok := source[key].([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(values))
	for _, value := range values {
		if item, ok := value.(map[string]any); ok {
			out = append(out, item)
		}
	}
	return out
}

func stringValue(source any, key string) string {
	mapped, ok := source.(map[string]any)
	if !ok || mapped == nil {
		return ""
	}
	value, ok := mapped[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func intValue(source map[string]any, key string) int {
	value, ok := source[key]
	if !ok || value == nil {
		return 0
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		parsed, _ := strconv.Atoi(string(typed))
		return parsed
	default:
		parsed, _ := strconv.Atoi(strings.TrimSpace(fmt.Sprint(typed)))
		return parsed
	}
}

func boolValue(source map[string]any, key string) bool {
	value, ok := source[key]
	if !ok || value == nil {
		return false
	}
	typed, _ := value.(bool)
	return typed
}

func normalizeInt(input map[string]any, key string, fallback int, min int, max int) int {
	value, ok := input[key]
	if !ok || value == nil {
		return fallback
	}
	var parsed int
	switch typed := value.(type) {
	case int:
		parsed = typed
	case int64:
		parsed = int(typed)
	case float64:
		parsed = int(typed)
	case json.Number:
		parsed, _ = strconv.Atoi(string(typed))
	default:
		parsed, _ = strconv.Atoi(strings.TrimSpace(fmt.Sprint(typed)))
	}
	if parsed < min {
		return fallback
	}
	if parsed > max {
		return max
	}
	return parsed
}

func copyMap(input map[string]any) map[string]any {
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func splitLines(value string) []string {
	var result []string
	for _, line := range strings.Split(value, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && validKubeName(trimmed) {
			result = append(result, trimmed)
		}
	}
	return result
}

func ageText(createdAt string) string {
	return createdAt
}

func truncateString(value string, maxBytes int) string {
	if maxBytes < 1 || len(value) <= maxBytes {
		return value
	}
	return value[:maxBytes] + "\n... truncated ..."
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
