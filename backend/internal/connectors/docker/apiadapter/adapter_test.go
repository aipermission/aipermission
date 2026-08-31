package apiadapter

import (
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	dockerconnector "github.com/aipermission/aipermission/backend/internal/connectors/docker"
)

func TestDockerExecShellCommandUsesValidatedExecutable(t *testing.T) {
	target := connectors.TargetView{Config: map[string]any{"docker_command": "/usr/local/bin/docker-wrapper"}}
	command, err := dockerconnector.DockerShellCommand(target)
	if err != nil {
		t.Fatalf("resolve Docker command: %v", err)
	}
	got := dockerExecShellCommand(command, "api")
	if !strings.Contains(got, "'/usr/local/bin/docker-wrapper' exec -it -- 'api'") || !strings.Contains(got, "Server.Version") {
		t.Fatalf("unexpected live-console command: %q", got)
	}
}

func TestDockerExecShellCommandRejectsUnsafeExecutableBeforeConstruction(t *testing.T) {
	for _, unsafe := range []string{"docker --debug", "docker; id", "$(id)", "eval", "true", "podman-docker"} {
		if _, err := dockerconnector.DockerCommand(connectors.TargetView{Config: map[string]any{"docker_command": unsafe}}); err == nil {
			t.Fatalf("expected %q to be rejected", unsafe)
		}
	}
}

func TestDockerExecShellCommandQuotesContainerAndRejectsInvalidRef(t *testing.T) {
	if dockerconnector.ValidContainerRef("; id >/tmp/pwn; #") {
		t.Fatal("expected shell payload to be rejected as a container ref")
	}
	if !dockerconnector.ValidContainerRef("api-1.2") {
		t.Fatal("expected normal Docker container name to be accepted")
	}
}
