package connectortargets

import (
	"context"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	sshconnector "github.com/aipermission/aipermission/backend/internal/connectors/ssh"
)

func TestActionServicePreparesSSHExec(t *testing.T) {
	database := openTargetTestDB(t)
	keyID := insertTargetTestSSHKey(t, database, "main")
	store := NewStore(database)
	target, profile := createTargetTestSSHProfile(t, context.Background(), store, keyID, "core-1", "admin", "10.0.0.10", 2222)
	targetRef := TargetProfileRef("ssh", target.ID, profile.ID)
	registry := newTargetTestRegistry(t)
	service := actions.NewService(registry, NewResolver(database))

	prepared, err := service.Prepare(context.Background(), actions.PrepareRequest{
		Source:     "mcp",
		TargetRef:  targetRef,
		ActionName: sshconnector.ActionExec,
		Input:      map[string]any{"command": "hostname"},
		Reason:     "smoke",
		CreatedAt:  time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("prepare ssh exec: %v", err)
	}

	if prepared.Action.ConnectorKind != sshconnector.Kind {
		t.Fatalf("connector kind = %q", prepared.Action.ConnectorKind)
	}
	if prepared.Action.TargetRef != targetRef {
		t.Fatalf("target ref = %q", prepared.Action.TargetRef)
	}
	if prepared.Action.ProfileID < 1 {
		t.Fatalf("profile id = %d", prepared.Action.ProfileID)
	}
	if prepared.Action.Risk != connectors.RiskWrite {
		t.Fatalf("risk = %q", prepared.Action.Risk)
	}
	if prepared.Action.Payload["command"] != "hostname" {
		t.Fatalf("payload = %#v", prepared.Action.Payload)
	}
}

func TestActionServicePreparesSSHReadConsole(t *testing.T) {
	database := openTargetTestDB(t)
	keyID := insertTargetTestSSHKey(t, database, "main")
	store := NewStore(database)
	target, profile := createTargetTestSSHProfile(t, context.Background(), store, keyID, "core-1", "admin", "10.0.0.10", 2222)
	targetRef := TargetProfileRef("ssh", target.ID, profile.ID)
	registry := newTargetTestRegistry(t)
	service := actions.NewService(registry, NewResolver(database))

	prepared, err := service.Prepare(context.Background(), actions.PrepareRequest{
		Source:     "mcp",
		TargetRef:  targetRef,
		ActionName: sshconnector.ActionReadConsole,
		Input:      map[string]any{"tail_bytes": 4096},
	})
	if err != nil {
		t.Fatalf("prepare ssh read_console: %v", err)
	}

	if prepared.Action.ProfileID < 1 {
		t.Fatalf("profile id = %d", prepared.Action.ProfileID)
	}
	if prepared.Action.Risk != connectors.RiskRead {
		t.Fatalf("risk = %q", prepared.Action.Risk)
	}
	if prepared.Action.Payload["tail_bytes"] != 4096 {
		t.Fatalf("payload = %#v", prepared.Action.Payload)
	}
}

func newTargetTestRegistry(t *testing.T) *connectors.Registry {
	t.Helper()
	registry := connectors.NewRegistry()
	if err := registry.Register(sshconnector.New()); err != nil {
		t.Fatalf("register ssh connector: %v", err)
	}
	return registry
}
