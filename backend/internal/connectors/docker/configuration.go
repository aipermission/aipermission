package dockerconnector

import (
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/boundedtext"
	"github.com/aipermission/aipermission/backend/internal/connectors"
)

var (
	dockerAbsoluteCommandPattern = regexp.MustCompile(`^/[A-Za-z0-9_./+-]+$`)
	dockerContainerRefPattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,254}$`)
	dockerContainerIDPrefix      = regexp.MustCompile(`^[a-fA-F0-9]{6,64}$`)
)

type dockerScope struct {
	mode     string
	exact    []string
	patterns []string
}

func dockerScopeFromProfile(profile connectors.CredentialProfileView) dockerScope {
	return dockerScope{
		mode:     scopeMode(profile),
		exact:    splitLines(stringValue(profile.Public, "allowed_containers")),
		patterns: splitLines(stringValue(profile.Public, "allowed_patterns")),
	}
}

// ValidContainerRef accepts Docker container names and IDs without allowing
// shell syntax into the live-console force command.
func ValidContainerRef(containerRef string) bool {
	return dockerContainerRefPattern.MatchString(strings.TrimSpace(containerRef))
}

func (scope dockerScope) allowsInInventory(container DockerContainer, inventory []DockerContainer) bool {
	if scope.mode != "selected" {
		return true
	}
	for _, allowed := range scope.exact {
		if ambiguousContainerSelector(inventory, allowed) {
			continue
		}
		if allowed == container.Name || allowed == container.ID {
			return true
		}
		if dockerContainerIDPrefix.MatchString(allowed) && uniqueContainerIDPrefix(inventory, allowed, container.ID) {
			return true
		}
	}
	for _, pattern := range scope.patterns {
		if ok, _ := path.Match(pattern, container.Name); ok {
			return true
		}
	}
	return false
}

func ambiguousContainerSelector(containers []DockerContainer, selector string) bool {
	matchedID := ""
	for _, container := range containers {
		matches := container.Name == selector || container.ID == selector ||
			(dockerContainerIDPrefix.MatchString(selector) && strings.HasPrefix(strings.ToLower(container.ID), strings.ToLower(selector)))
		if !matches {
			continue
		}
		if matchedID != "" && !strings.EqualFold(matchedID, container.ID) {
			return true
		}
		matchedID = container.ID
	}
	return false
}

func (scope dockerScope) filter(containers []DockerContainer) []DockerContainer {
	if scope.mode != "selected" {
		return containers
	}
	filtered := make([]DockerContainer, 0, len(containers))
	for _, container := range containers {
		if scope.allowsInInventory(container, containers) {
			filtered = append(filtered, container)
		}
	}
	return filtered
}

func uniqueContainerIDPrefix(containers []DockerContainer, prefix string, expectedID string) bool {
	prefix = strings.ToLower(prefix)
	expectedID = strings.ToLower(expectedID)
	matches := 0
	matchedID := ""
	for _, container := range containers {
		id := strings.ToLower(strings.TrimSpace(container.ID))
		if strings.HasPrefix(id, prefix) {
			matches++
			matchedID = id
		}
	}
	return matches == 1 && matchedID == expectedID
}

func scopeMode(profile connectors.CredentialProfileView) string {
	if strings.TrimSpace(stringValue(profile.Public, "scope_mode")) == "selected" {
		return "selected"
	}
	return "all"
}

func ConnectionMode(target connectors.TargetView) string {
	mode := strings.TrimSpace(stringValue(target.Config, "connection_mode"))
	if mode == "" {
		return "over_ssh"
	}
	return mode
}

// DockerCommand resolves the configured Docker executable. Only the standard
// docker binary or an absolute wrapper path is accepted; arguments belong to
// connector-owned command templates.
func DockerCommand(target connectors.TargetView) (string, error) {
	command := strings.TrimSpace(stringValue(target.Config, "docker_command"))
	if command == "" {
		command = defaultDockerCommand
	}
	if command == defaultDockerCommand {
		return command, nil
	}
	if len(command) > 1024 || !filepath.IsAbs(command) || !dockerAbsoluteCommandPattern.MatchString(command) || strings.Contains(command, "/../") || strings.HasSuffix(command, "/..") {
		return "", fmt.Errorf("%w: docker_command must be docker or an absolute wrapper path without arguments; replace legacy values such as 'sudo docker' or 'docker --context ...' with a wrapper script path", ErrInvalidConfig)
	}
	return command, nil
}

// DockerShellCommand renders the validated executable for connector-owned
// shell templates. `command docker` bypasses remote aliases and functions.
func DockerShellCommand(target connectors.TargetView) (string, error) {
	command, err := DockerCommand(target)
	if err != nil {
		return "", err
	}
	if command == defaultDockerCommand {
		return "command docker", nil
	}
	return shellQuote(command), nil
}

func normalizeContainerInput(input map[string]any) (string, error) {
	container := strings.TrimSpace(stringValue(input, "container"))
	if container == "" {
		return "", fmt.Errorf("container is required")
	}
	if strings.ContainsAny(container, "\x00\n\r") {
		return "", fmt.Errorf("container contains unsupported characters")
	}
	return container, nil
}

func normalizeExecCommandInput(input map[string]any) (string, error) {
	command := strings.TrimSpace(stringValue(input, "command"))
	if command == "" {
		return "", fmt.Errorf("command is required")
	}
	if len(command) > maxExecCommandLen {
		return "", fmt.Errorf("command is larger than %d bytes", maxExecCommandLen)
	}
	if strings.ContainsRune(command, '\x00') {
		return "", fmt.Errorf("command contains unsupported characters")
	}
	return command, nil
}

func normalizeDockerOptionInput(input map[string]any, key string) string {
	value := strings.TrimSpace(stringValue(input, key))
	if value == "" || strings.ContainsAny(value, "\x00\n\r") {
		return ""
	}
	return value
}

func firstLine(value string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(value), "\n")
	return boundedtext.TruncateUTF8(line, 120, "...")
}

func dockerCommandError(command string, result connectors.CommandRunResult) error {
	message := strings.TrimSpace(result.Stderr)
	if message == "" {
		message = strings.TrimSpace(result.Stdout)
	}
	if message == "" {
		message = fmt.Sprintf("%s failed with exit code %d", command, result.ExitCode)
	}
	return fmt.Errorf("%s failed: %s", command, truncateString(message, 4000))
}
