//go:build windows

package dockerconnector

import (
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestDockerCommandUsesRemotePOSIXPathRulesOnWindows(t *testing.T) {
	target := connectors.TargetView{Config: map[string]any{"docker_command": "/usr/local/bin/docker-wrapper"}}
	command, err := DockerCommand(target)
	if err != nil {
		t.Fatalf("remote POSIX wrapper path: %v", err)
	}
	if command != "/usr/local/bin/docker-wrapper" {
		t.Fatalf("command = %q", command)
	}
}
