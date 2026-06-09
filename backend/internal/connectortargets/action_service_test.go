package connectortargets

import (
	"context"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/builtin"
	sshconnector "github.com/aipermission/aipermission/backend/internal/connectors/ssh"
)

func TestActionServicePreparesLegacySSHExec(t *testing.T) {
	database := openTargetTestDB(t)
	keyID := insertTargetTestSSHKey(t, database, "main")
	serverID := insertTargetTestServer(t, database, keyID)
	registry, err := builtin.NewRegistry()
	if err != nil {
		t.Fatalf("builtin registry: %v", err)
	}
	service := actions.NewService(registry, NewResolver(database))

	prepared, err := service.Prepare(context.Background(), actions.PrepareRequest{
		Source:     "mcp",
		TargetRef:  SSHTargetRef(serverID),
		ActionName: sshconnector.ActionExec,
		Input:      map[string]any{"command": "hostname"},
		Reason:     "smoke",
		CreatedAt:  time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("prepare legacy ssh exec: %v", err)
	}

	if prepared.Action.ConnectorKind != sshconnector.Kind {
		t.Fatalf("connector kind = %q", prepared.Action.ConnectorKind)
	}
	if prepared.Action.TargetRef != SSHTargetRef(serverID) {
		t.Fatalf("target ref = %q", prepared.Action.TargetRef)
	}
	if prepared.Action.ProfileID != keyID {
		t.Fatalf("profile id = %d", prepared.Action.ProfileID)
	}
	if prepared.Action.Risk != connectors.RiskWrite {
		t.Fatalf("risk = %q", prepared.Action.Risk)
	}
	if prepared.Action.Payload["command"] != "hostname" {
		t.Fatalf("payload = %#v", prepared.Action.Payload)
	}
}

func TestActionServicePreparesLegacySSHReadConsole(t *testing.T) {
	database := openTargetTestDB(t)
	keyID := insertTargetTestSSHKey(t, database, "main")
	serverID := insertTargetTestServer(t, database, keyID)
	registry, err := builtin.NewRegistry()
	if err != nil {
		t.Fatalf("builtin registry: %v", err)
	}
	service := actions.NewService(registry, NewResolver(database))

	prepared, err := service.Prepare(context.Background(), actions.PrepareRequest{
		Source:     "mcp",
		TargetRef:  SSHTargetRef(serverID),
		ActionName: sshconnector.ActionReadConsole,
		Input:      map[string]any{"tail_bytes": 4096},
	})
	if err != nil {
		t.Fatalf("prepare legacy ssh read_console: %v", err)
	}

	if prepared.Action.ProfileID != keyID {
		t.Fatalf("profile id = %d", prepared.Action.ProfileID)
	}
	if prepared.Action.Risk != connectors.RiskRead {
		t.Fatalf("risk = %q", prepared.Action.Risk)
	}
	if prepared.Action.Payload["tail_bytes"] != 4096 {
		t.Fatalf("payload = %#v", prepared.Action.Payload)
	}
}
