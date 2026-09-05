package kubernetesconnector

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

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

func isKubeConflictResult(result connectors.CommandRunResult) bool {
	text := strings.ToLower(result.Stderr + "\n" + result.Stdout)
	return strings.Contains(text, "error from server (conflict)")
}

func isKubeDefiniteMutationFailure(result connectors.CommandRunResult) bool {
	text := strings.ToLower(result.Stderr + "\n" + result.Stdout)
	for _, marker := range []string{
		"error from server (forbidden)",
		"error from server (unauthorized)",
		"error from server (notfound)",
		"error from server (invalid)",
		"error from server (badrequest)",
		"error from server (unprocessableentity)",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
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

func boundedIntOrDefault(input map[string]any, key string, fallback int, min int, max int) int {
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
