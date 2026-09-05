package kubernetesconnector

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

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
	command, err := KubectlCommand(runtime.Target)
	if err != nil {
		return nil, err
	}
	return &kubeClient{
		runtime:   runtime,
		transport: transport,
		command:   command,
		context:   strings.TrimSpace(stringValue(runtime.Target.Config, "context")),
		scope:     kubeScopeFromProfile(runtime.Profile),
	}, nil
}

// KubectlCommand resolves and validates the connector-owned executable used by
// both bounded actions and live-console sessions.
func KubectlCommand(target connectors.TargetView) (string, error) {
	command := strings.TrimSpace(stringValue(target.Config, "kubectl_command"))
	if command == "" {
		command = defaultKubectlCommand
	}
	if len(command) > 1024 || !kubectlCommandPattern.MatchString(command) {
		return "", fmt.Errorf("%w: kubectl_command must be an executable name or wrapper path", ErrInvalidConfig)
	}
	return command, nil
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
		SourceTargetRef:    client.runtime.Target.Ref,
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
	limit := boundedIntOrDefault(input, "limit", 200, 1, 1000)
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
	tail := boundedIntOrDefault(input, "tail", defaultLogTail, 1, maxLogTail)
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
	resourceVersion := strings.TrimSpace(stringValue(input, "expected_resource_version"))
	if len(resourceVersion) > 256 || strings.ContainsAny(resourceVersion, "\r\n") {
		return connectors.ActionResult{}, fmt.Errorf("expected_resource_version is invalid")
	}
	command := fmt.Sprintf("%s rollout restart deployment/%s -n %s 2>&1", client.baseCommand(), shellQuote(deployment), shellQuote(namespace))
	commandLabel := "kubectl rollout restart"
	if resourceVersion != "" {
		patch, err := conditionalRolloutRestartPatch(resourceVersion, time.Now().UTC())
		if err != nil {
			return connectors.ActionResult{}, fmt.Errorf("encode rollout restart patch: %w", err)
		}
		command = fmt.Sprintf("%s patch deployment %s -n %s --type=merge -p %s -o json 2>&1", client.baseCommand(), shellQuote(deployment), shellQuote(namespace), shellQuote(patch))
		commandLabel = "kubectl conditional rollout restart"
	}
	result, err := client.run(ctx, command, 30)
	if err != nil {
		if !result.DispatchStarted {
			return connectors.ActionResult{}, err
		}
		return connectors.ActionResult{}, connectors.ClassifyOutcomeUnknown(
			"command_transport",
			nil,
			fmt.Errorf("%s outcome is unknown after transport failure: %w", commandLabel, err),
		)
	}
	if result.ExitCode != 0 {
		err := kubeCommandError(commandLabel, result)
		if resourceVersion != "" && isKubeConflictResult(result) {
			return connectors.ActionResult{}, connectors.ClassifyError("precondition_failed", err)
		}
		if result.DispatchStarted && !isKubeDefiniteMutationFailure(result) {
			return connectors.ActionResult{}, connectors.ClassifyOutcomeUnknown(
				"kubectl_response",
				map[string]any{"exit_code": result.ExitCode},
				fmt.Errorf("%s outcome is unknown after kubectl transport failure: %w", commandLabel, err),
			)
		}
		return connectors.ActionResult{}, err
	}
	return connectors.ActionResult{Status: connectors.ResultCompleted, Output: map[string]any{"namespace": namespace, "deployment": deployment, "expected_resource_version": resourceVersion, "response": strings.TrimSpace(result.Stdout), "duration_ms": result.DurationMS}, DisplayText: strings.TrimSpace(result.Stdout)}, nil
}

func conditionalRolloutRestartPatch(resourceVersion string, restartedAt time.Time) (string, error) {
	patch, err := json.Marshal(map[string]any{
		"metadata": map[string]any{"resourceVersion": resourceVersion},
		"spec": map[string]any{"template": map[string]any{"metadata": map[string]any{"annotations": map[string]any{
			"kubectl.kubernetes.io/restartedAt": restartedAt.UTC().Format(time.RFC3339Nano),
		}}}},
	})
	if err != nil {
		return "", err
	}
	return string(patch), nil
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
