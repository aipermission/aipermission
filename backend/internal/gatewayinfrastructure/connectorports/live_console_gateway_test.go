package connectorports

import (
	"errors"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func TestLiveConsoleCommandSourceIsBoundToInvokingTargetProfile(t *testing.T) {
	if err := requireCommandSourceTarget("docker:1:2", "docker:1:2"); err != nil {
		t.Fatal(err)
	}
	for _, targetRef := range []string{"docker:1:3", "docker:2:2", "ssh:1:2", "postgres:1:2", "invalid", ""} {
		if err := requireCommandSourceTarget("docker:1:2", targetRef); !errors.Is(err, connectortargets.ErrInvalidTargetRef) {
			t.Fatalf("source %q error = %v", targetRef, err)
		}
	}
}

func TestLiveConsoleGatewayBindsSourceTargetProfile(t *testing.T) {
	gateway := (&PortsComponent{}).LiveConsoleGateway(Workspace{}, " docker:1:2 ")
	if gateway.sourceTargetRef != "docker:1:2" {
		t.Fatalf("source target ref = %q, want docker:1:2", gateway.sourceTargetRef)
	}
}
