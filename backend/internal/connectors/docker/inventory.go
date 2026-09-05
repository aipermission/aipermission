package dockerconnector

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

type DockerContainer struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Image          string `json:"image"`
	Command        string `json:"command,omitempty"`
	State          string `json:"state"`
	Status         string `json:"status"`
	Health         string `json:"health,omitempty"`
	Ports          string `json:"ports,omitempty"`
	Labels         string `json:"labels,omitempty"`
	ComposeProject string `json:"compose_project,omitempty"`
	ComposeService string `json:"compose_service,omitempty"`
}

type DockerImage struct {
	ID           string `json:"id,omitempty"`
	Repository   string `json:"repository,omitempty"`
	Tag          string `json:"tag,omitempty"`
	Digest       string `json:"digest,omitempty"`
	CreatedAt    string `json:"created_at,omitempty"`
	CreatedSince string `json:"created_since,omitempty"`
	Size         string `json:"size,omitempty"`
	Containers   int    `json:"containers,omitempty"`
}

type DockerNetwork struct {
	ID         string `json:"id,omitempty"`
	Name       string `json:"name"`
	Driver     string `json:"driver,omitempty"`
	Scope      string `json:"scope,omitempty"`
	IPv6       string `json:"ipv6,omitempty"`
	Internal   string `json:"internal,omitempty"`
	Labels     string `json:"labels,omitempty"`
	Containers int    `json:"containers,omitempty"`
}

type DockerVolume struct {
	Name       string `json:"name"`
	Driver     string `json:"driver,omitempty"`
	Scope      string `json:"scope,omitempty"`
	Mountpoint string `json:"mountpoint,omitempty"`
	Labels     string `json:"labels,omitempty"`
	Containers int    `json:"containers,omitempty"`
}

func (image DockerImage) Ref() string {
	repository := strings.TrimSpace(image.Repository)
	tag := strings.TrimSpace(image.Tag)
	if repository == "" {
		return strings.TrimSpace(image.ID)
	}
	if tag == "" || tag == "<none>" {
		return repository
	}
	return repository + ":" + tag
}

func (container DockerContainer) Ref() string {
	if strings.TrimSpace(container.Name) != "" {
		return container.Name
	}
	return container.ID
}

func (client *dockerClient) listContainers(ctx context.Context, includeStopped bool) ([]DockerContainer, error) {
	args := "ps --no-trunc --format '{{json .}}'"
	if includeStopped {
		args = "ps -a --no-trunc --format '{{json .}}'"
	}
	result, err := client.run(ctx, client.command+" "+args, 20)
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		return nil, dockerCommandError("docker ps", result)
	}
	containers, err := parseDockerPS(result.Stdout)
	if err != nil {
		return nil, err
	}
	filtered := containers[:0]
	for _, container := range containers {
		if client.scope.allows(container) {
			filtered = append(filtered, container)
		}
	}
	return filtered, nil
}

func (client *dockerClient) listImages(ctx context.Context) ([]DockerImage, error) {
	result, err := client.run(ctx, client.command+" image ls --no-trunc --format '{{json .}}'", 20)
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		return nil, dockerCommandError("docker image ls", result)
	}
	images, err := parseDockerImages(result.Stdout)
	if err != nil {
		return nil, err
	}
	containers, err := client.listContainers(ctx, true)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, container := range containers {
		imageRef := strings.TrimSpace(container.Image)
		if imageRef == "" {
			continue
		}
		counts[imageRef]++
	}
	filtered := images[:0]
	for _, image := range images {
		ref := image.Ref()
		count := counts[ref]
		if count == 0 && image.Tag != "" {
			count = counts[image.Repository+":"+image.Tag]
		}
		if client.scope.mode == "selected" && count == 0 {
			continue
		}
		image.Containers = count
		filtered = append(filtered, image)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		return strings.ToLower(filtered[i].Ref()) < strings.ToLower(filtered[j].Ref())
	})
	return filtered, nil
}

func (client *dockerClient) listNetworks(ctx context.Context) ([]DockerNetwork, error) {
	if client.scope.mode == "selected" {
		return client.scopedNetworksFromInspect(ctx)
	}
	result, err := client.run(ctx, client.command+" network ls --no-trunc --format '{{json .}}'", 20)
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		return nil, dockerCommandError("docker network ls", result)
	}
	networks, err := parseDockerNetworks(result.Stdout)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(networks, func(i, j int) bool {
		return strings.ToLower(networks[i].Name) < strings.ToLower(networks[j].Name)
	})
	return networks, nil
}

func (client *dockerClient) listVolumes(ctx context.Context) ([]DockerVolume, error) {
	if client.scope.mode == "selected" {
		return client.scopedVolumesFromInspect(ctx)
	}
	result, err := client.run(ctx, client.command+" volume ls --format '{{json .}}'", 20)
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		return nil, dockerCommandError("docker volume ls", result)
	}
	volumes, err := parseDockerVolumes(result.Stdout)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(volumes, func(i, j int) bool {
		return strings.ToLower(volumes[i].Name) < strings.ToLower(volumes[j].Name)
	})
	return volumes, nil
}

func (client *dockerClient) scopedNetworksFromInspect(ctx context.Context) ([]DockerNetwork, error) {
	inspect, err := client.inspectScopedContainers(ctx)
	if err != nil {
		return nil, err
	}
	byName := map[string]DockerNetwork{}
	for _, item := range inspect {
		settings, _ := item["NetworkSettings"].(map[string]any)
		networks, _ := settings["Networks"].(map[string]any)
		for name, raw := range networks {
			network := byName[name]
			network.Name = name
			network.Scope = "container"
			network.Containers++
			if data, ok := raw.(map[string]any); ok {
				if network.ID == "" {
					network.ID = strings.TrimSpace(stringValue(data, "NetworkID"))
				}
				if network.Driver == "" {
					network.Driver = strings.TrimSpace(stringValue(data, "Driver"))
				}
			}
			byName[name] = network
		}
	}
	networks := make([]DockerNetwork, 0, len(byName))
	for _, network := range byName {
		networks = append(networks, network)
	}
	sort.SliceStable(networks, func(i, j int) bool {
		return strings.ToLower(networks[i].Name) < strings.ToLower(networks[j].Name)
	})
	return networks, nil
}

func (client *dockerClient) scopedVolumesFromInspect(ctx context.Context) ([]DockerVolume, error) {
	inspect, err := client.inspectScopedContainers(ctx)
	if err != nil {
		return nil, err
	}
	byName := map[string]DockerVolume{}
	for _, item := range inspect {
		mounts, _ := item["Mounts"].([]any)
		for _, raw := range mounts {
			mount, _ := raw.(map[string]any)
			if strings.TrimSpace(stringValue(mount, "Type")) != "volume" {
				continue
			}
			name := strings.TrimSpace(stringValue(mount, "Name"))
			if name == "" {
				continue
			}
			volume := byName[name]
			volume.Name = name
			volume.Driver = strings.TrimSpace(stringValue(mount, "Driver"))
			volume.Mountpoint = strings.TrimSpace(stringValue(mount, "Source"))
			volume.Scope = "container"
			volume.Containers++
			byName[name] = volume
		}
	}
	volumes := make([]DockerVolume, 0, len(byName))
	for _, volume := range byName {
		volumes = append(volumes, volume)
	}
	sort.SliceStable(volumes, func(i, j int) bool {
		return strings.ToLower(volumes[i].Name) < strings.ToLower(volumes[j].Name)
	})
	return volumes, nil
}

func (client *dockerClient) inspectScopedContainers(ctx context.Context) ([]map[string]any, error) {
	containers, err := client.listContainers(ctx, true)
	if err != nil {
		return nil, err
	}
	if len(containers) == 0 {
		return nil, nil
	}
	args := make([]string, 0, len(containers))
	for _, container := range containers {
		args = append(args, shellQuote(container.Ref()))
	}
	result, err := client.run(ctx, client.command+" inspect -- "+strings.Join(args, " "), 30)
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		return nil, dockerCommandError("docker inspect", result)
	}
	if len(result.Stdout) > maxInspectBytes {
		return nil, fmt.Errorf("docker inspect output is larger than %d bytes", maxInspectBytes)
	}
	var inspect []map[string]any
	if err := json.Unmarshal([]byte(result.Stdout), &inspect); err != nil {
		return nil, fmt.Errorf("parse docker inspect output: %w", err)
	}
	return inspect, nil
}

func (client *dockerClient) resolveContainer(ctx context.Context, requested string) (DockerContainer, error) {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return DockerContainer{}, fmt.Errorf("container is required")
	}
	containers, err := client.listContainers(ctx, true)
	if err != nil {
		return DockerContainer{}, err
	}
	for _, container := range containers {
		if container.Name == requested || container.ID == requested || strings.HasPrefix(container.ID, requested) {
			return container, nil
		}
	}
	return DockerContainer{}, fmt.Errorf("%w: %s", ErrScopeDenied, requested)
}

func parseDockerPS(data string) ([]DockerContainer, error) {
	var containers []DockerContainer
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, fmt.Errorf("parse docker ps row: %w", err)
		}
		container := DockerContainer{
			ID:      strings.TrimSpace(stringValue(row, "ID")),
			Name:    strings.TrimSpace(stringValue(row, "Names")),
			Image:   strings.TrimSpace(stringValue(row, "Image")),
			Command: strings.TrimSpace(stringValue(row, "Command")),
			State:   strings.TrimSpace(stringValue(row, "State")),
			Status:  strings.TrimSpace(stringValue(row, "Status")),
			Ports:   strings.TrimSpace(stringValue(row, "Ports")),
			Labels:  strings.TrimSpace(stringValue(row, "Labels")),
		}
		labels := parseLabelString(container.Labels)
		container.ComposeProject = labels["com.docker.compose.project"]
		container.ComposeService = labels["com.docker.compose.service"]
		container.Health = parseHealth(container.Status)
		if container.ID == "" && container.Name == "" {
			continue
		}
		containers = append(containers, container)
	}
	return containers, nil
}

func parseDockerImages(data string) ([]DockerImage, error) {
	var images []DockerImage
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, fmt.Errorf("parse docker image row: %w", err)
		}
		image := DockerImage{
			ID:           strings.TrimSpace(stringValue(row, "ID")),
			Repository:   strings.TrimSpace(stringValue(row, "Repository")),
			Tag:          strings.TrimSpace(stringValue(row, "Tag")),
			Digest:       strings.TrimSpace(stringValue(row, "Digest")),
			CreatedAt:    strings.TrimSpace(stringValue(row, "CreatedAt")),
			CreatedSince: strings.TrimSpace(stringValue(row, "CreatedSince")),
			Size:         strings.TrimSpace(stringValue(row, "Size")),
		}
		if image.Repository == "" && image.ID == "" {
			continue
		}
		images = append(images, image)
	}
	return images, nil
}

func parseDockerNetworks(data string) ([]DockerNetwork, error) {
	var networks []DockerNetwork
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, fmt.Errorf("parse docker network row: %w", err)
		}
		network := DockerNetwork{
			ID:       strings.TrimSpace(stringValue(row, "ID")),
			Name:     strings.TrimSpace(stringValue(row, "Name")),
			Driver:   strings.TrimSpace(stringValue(row, "Driver")),
			Scope:    strings.TrimSpace(stringValue(row, "Scope")),
			IPv6:     strings.TrimSpace(stringValue(row, "IPv6")),
			Internal: strings.TrimSpace(stringValue(row, "Internal")),
			Labels:   strings.TrimSpace(stringValue(row, "Labels")),
		}
		if network.Name == "" && network.ID == "" {
			continue
		}
		networks = append(networks, network)
	}
	return networks, nil
}

func parseDockerVolumes(data string) ([]DockerVolume, error) {
	var volumes []DockerVolume
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, fmt.Errorf("parse docker volume row: %w", err)
		}
		volume := DockerVolume{
			Name:       strings.TrimSpace(stringValue(row, "Name")),
			Driver:     strings.TrimSpace(stringValue(row, "Driver")),
			Scope:      strings.TrimSpace(stringValue(row, "Scope")),
			Mountpoint: strings.TrimSpace(stringValue(row, "Mountpoint")),
			Labels:     strings.TrimSpace(stringValue(row, "Labels")),
		}
		if volume.Name == "" {
			continue
		}
		volumes = append(volumes, volume)
	}
	return volumes, nil
}

func containersDisplay(containers []DockerContainer) string {
	if len(containers) == 0 {
		return "No Docker containers matched this profile scope."
	}
	lines := make([]string, 0, len(containers))
	for _, container := range containers {
		meta := []string{container.State}
		if container.Health != "" {
			meta = append(meta, container.Health)
		}
		if container.ComposeProject != "" {
			meta = append(meta, "project="+container.ComposeProject)
		}
		lines = append(lines, fmt.Sprintf("%s\t%s\t%s\t%s", container.Name, container.Image, strings.Join(meta, " "), container.Status))
	}
	return strings.Join(lines, "\n")
}

func imagesDisplay(images []DockerImage) string {
	if len(images) == 0 {
		return "No Docker images matched this profile scope."
	}
	lines := make([]string, 0, len(images))
	for _, image := range images {
		lines = append(lines, fmt.Sprintf("%s\t%s\tcontainers=%d", image.Ref(), image.Size, image.Containers))
	}
	return strings.Join(lines, "\n")
}

func networksDisplay(networks []DockerNetwork) string {
	if len(networks) == 0 {
		return "No Docker networks matched this profile scope."
	}
	lines := make([]string, 0, len(networks))
	for _, network := range networks {
		lines = append(lines, fmt.Sprintf("%s\t%s\t%s\tcontainers=%d", network.Name, network.Driver, network.Scope, network.Containers))
	}
	return strings.Join(lines, "\n")
}

func volumesDisplay(volumes []DockerVolume) string {
	if len(volumes) == 0 {
		return "No Docker volumes matched this profile scope."
	}
	lines := make([]string, 0, len(volumes))
	for _, volume := range volumes {
		lines = append(lines, fmt.Sprintf("%s\t%s\t%s\tcontainers=%d", volume.Name, volume.Driver, volume.Scope, volume.Containers))
	}
	return strings.Join(lines, "\n")
}

func parseLabelString(value string) map[string]string {
	labels := map[string]string{}
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, val, found := strings.Cut(part, "=")
		if !found {
			labels[part] = ""
			continue
		}
		labels[strings.TrimSpace(key)] = strings.TrimSpace(val)
	}
	return labels
}

func parseHealth(status string) string {
	lower := strings.ToLower(status)
	switch {
	case strings.Contains(lower, "(healthy)"):
		return "healthy"
	case strings.Contains(lower, "(unhealthy)"):
		return "unhealthy"
	case strings.Contains(lower, "(health: starting)"):
		return "starting"
	default:
		return ""
	}
}

func redactInspect(items []map[string]any) []map[string]any {
	for _, item := range items {
		if config, ok := item["Config"].(map[string]any); ok {
			if env, ok := config["Env"].([]any); ok {
				redacted := make([]any, 0, len(env))
				for _, value := range env {
					text := fmt.Sprint(value)
					name, _, found := strings.Cut(text, "=")
					if found && strings.TrimSpace(name) != "" {
						redacted = append(redacted, name+"=***")
					} else {
						redacted = append(redacted, "***")
					}
				}
				config["Env"] = redacted
			}
		}
	}
	return items
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func splitLines(value string) []string {
	var out []string
	for _, line := range strings.Split(value, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}

func copyMap(in map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range in {
		out[key] = value
	}
	return out
}

func stringValue(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	switch value := values[key].(type) {
	case string:
		return value
	case fmt.Stringer:
		return value.String()
	case nil:
		return ""
	default:
		return fmt.Sprint(value)
	}
}

func boolValue(values map[string]any, key string, fallback bool) bool {
	if values == nil {
		return fallback
	}
	switch value := values[key].(type) {
	case bool:
		return value
	case string:
		parsed, err := strconv.ParseBool(value)
		if err == nil {
			return parsed
		}
	case float64:
		return value != 0
	case int:
		return value != 0
	}
	return fallback
}

func normalizeInt(values map[string]any, key string, fallback int, minValue int, maxValue int) int {
	value := intValue(values, key, fallback)
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func intValue(values map[string]any, key string, fallback int) int {
	if values == nil {
		return fallback
	}
	if parsed, ok := connectors.NativeIntValue(values[key]); ok {
		return parsed
	}
	return fallback
}

func truncateString(value string, maxBytes int) string {
	if maxBytes <= 0 || len(value) <= maxBytes {
		return value
	}
	return value[:maxBytes] + "\n...[truncated]"
}
