package dockerconnector

import (
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

var (
	dockerAbsoluteCommandPattern = regexp.MustCompile(`^/[A-Za-z0-9_./+-]+$`)
	dockerContainerRefPattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,254}$`)
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

func ProfileAllowsContainerRef(profile connectors.CredentialProfileView, containerRef string) bool {
	containerRef = strings.TrimSpace(containerRef)
	if !ValidContainerRef(containerRef) {
		return false
	}
	return dockerScopeFromProfile(profile).allows(DockerContainer{ID: containerRef, Name: containerRef})
}

// ValidContainerRef accepts Docker container names and IDs without allowing
// shell syntax into the live-console force command.
func ValidContainerRef(containerRef string) bool {
	return dockerContainerRefPattern.MatchString(strings.TrimSpace(containerRef))
}

func (scope dockerScope) allows(container DockerContainer) bool {
	if scope.mode != "selected" {
		return true
	}
	candidates := []string{container.ID, container.Name}
	if len(container.ID) >= 12 {
		candidates = append(candidates, container.ID[:12])
	}
	for _, allowed := range scope.exact {
		for _, candidate := range candidates {
			if allowed == candidate || strings.HasPrefix(container.ID, allowed) {
				return true
			}
		}
	}
	for _, pattern := range scope.patterns {
		if ok, _ := path.Match(pattern, container.Name); ok {
			return true
		}
	}
	return false
}

func scopeMode(profile connectors.CredentialProfileView) string {
	if strings.TrimSpace(stringValue(profile.Public, "scope_mode")) == "selected" {
		return "selected"
	}
	return "all"
}

func connectionMode(target connectors.TargetView) string {
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
	if len(line) > 120 {
		return line[:117] + "..."
	}
	return line
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
