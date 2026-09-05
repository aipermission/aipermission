package kubernetesconnector

import (
	"context"
	"fmt"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

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
			Description: "kubectl executable name or wrapper path on the remote host. Shell arguments and operators are not accepted.",
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

func (Connector) ValidateTargetConfig(config map[string]any) error {
	_, err := KubectlCommand(connectors.TargetView{Config: config})
	return err
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
		{Name: ActionListEvents, Label: "List events", Description: "List warning-first Kubernetes events.", Category: "browser", Risk: connectors.RiskRead, InputSchema: connectors.Schema{Fields: []connectors.Field{{Name: "namespace", Label: "Namespace", Type: connectors.FieldString, Description: "Optional namespace. Empty lists across allowed namespaces."}, {Name: "limit", Label: "Limit", Type: connectors.FieldInteger, Default: 200}}}, OutputHint: connectors.OutputHint{Format: "json", MaxRows: 1000}},
		{Name: ActionDescribe, Label: "Describe resource", Description: "Read JSON metadata for one Kubernetes resource.", Category: "browser", Risk: connectors.RiskRead, InputSchema: connectors.Schema{Fields: []connectors.Field{{Name: "resource_type", Label: "Resource type", Type: connectors.FieldSelect, Required: true, Options: []connectors.FieldOption{{Value: "pod", Label: "Pod"}, {Value: "deployment", Label: "Deployment"}, {Value: "statefulset", Label: "StatefulSet"}, {Value: "daemonset", Label: "DaemonSet"}, {Value: "service", Label: "Service"}, {Value: "ingress", Label: "Ingress"}, {Value: "node", Label: "Node"}}}, {Name: "name", Label: "Name", Type: connectors.FieldString, Required: true}, {Name: "namespace", Label: "Namespace", Type: connectors.FieldString, Description: "Required for namespaced resources unless target default namespace is set."}}}, OutputHint: connectors.OutputHint{Format: "json", MaxBytes: maxKubectlBytes}},
		{Name: ActionLogs, Label: "Pod logs", Description: "Read a bounded tail of pod logs.", Category: "browser", Risk: connectors.RiskRead, InputSchema: connectors.Schema{Fields: []connectors.Field{{Name: "namespace", Label: "Namespace", Type: connectors.FieldString, Required: true}, {Name: "pod", Label: "Pod", Type: connectors.FieldString, Required: true}, {Name: "container", Label: "Container", Type: connectors.FieldString, Description: "Optional container name."}, {Name: "tail", Label: "Tail lines", Type: connectors.FieldInteger, Default: defaultLogTail}}}, OutputHint: connectors.OutputHint{Format: "text", MaxBytes: maxLogBytes}},
		{Name: ActionRolloutRestart, Label: "Rollout restart", Description: "Restart one Kubernetes deployment.", Category: "lifecycle", Risk: connectors.RiskWrite, InputSchema: connectors.Schema{Fields: []connectors.Field{{Name: "namespace", Label: "Namespace", Type: connectors.FieldString, Required: true}, {Name: "deployment", Label: "Deployment", Type: connectors.FieldString, Required: true}, {Name: "expected_resource_version", Label: "Expected resource version", Type: connectors.FieldString, Description: "Optional metadata.resourceVersion from describe_resource. When set, restart fails if the deployment changed."}}}, OutputHint: connectors.OutputHint{Format: "json", MaxBytes: 4000}},
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
		input["limit"] = boundedIntOrDefault(input, "limit", 200, 1, 1000)
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
		input["tail"] = boundedIntOrDefault(input, "tail", defaultLogTail, 1, maxLogTail)
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
		resourceVersion := strings.TrimSpace(stringValue(input, "expected_resource_version"))
		if len(resourceVersion) > 256 || strings.ContainsAny(resourceVersion, "\r\n") {
			return connectors.PreparedAction{}, fmt.Errorf("expected_resource_version is invalid")
		}
		input["expected_resource_version"] = resourceVersion
		title = "Rollout restart Kubernetes deployment"
		summary = fmt.Sprintf("%s/%s", namespace, deployment)
	default:
		return connectors.PreparedAction{}, ErrUnsupportedAction
	}
	prepared := connectors.PreparedAction{
		ConnectorKind: Kind,
		TargetRef:     req.Target.Ref,
		ProfileID:     req.Profile.ID,
		ActionName:    req.ActionName,
		Dependencies:  connectors.CommandTransportDependencies(req.Target),
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
	}
	if req.ActionName == ActionRolloutRestart && strings.TrimSpace(stringValue(input, "expected_resource_version")) != "" {
		prepared.RetryPolicy = connectors.ConditionalRetryPolicy("expected_resource_version")
	}
	return prepared, nil
}
