package connectorports

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/db"
)

func TestTargetAuditUsesScopedPublicTargetWithoutRuntimeSurface(t *testing.T) {
	database, err := db.OpenEncrypted(filepath.Join(t.TempDir(), "target-audit.db"), "target-audit-fixture-password")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	target, err := connectortargets.NewStore(database).CreateTarget(t.Context(), connectortargets.CreateTargetInput{
		ConnectorKind: "fixture", Name: "Target audit fixture", Config: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	workspace := NewWorkspace(nil, database, nil, nil)
	calls := 0
	details := map[string]any{"target_id": int64(999), "connector_kind": "spoofed", "reason": "public evidence"}
	workspace.Targets.TargetAudit = func(ctx context.Context, action string, payload any) error {
		calls++
		want := map[string]any{"target_id": target.ID, "connector_kind": target.ConnectorKind, "details": details}
		if ctx != t.Context() || action != "connector.fixture_attested" || !reflect.DeepEqual(payload, want) {
			t.Fatalf("audit scope came from caller/runtime: %#v", payload)
		}
		return nil
	}
	component := NewPorts(PortsDependencies{})
	gateway := component.TargetOperationGateway(workspace, target.ConnectorKind, target.ID)
	if err := gateway.ConnectorWriteTargetAudit(t.Context(), "connector.fixture_attested", details); err != nil || calls != 1 {
		t.Fatalf("target-only audit was dropped: calls %d error %v", calls, err)
	}
	var count int
	if err := database.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM connector_runtime_surfaces").Scan(&count); err != nil || count != 0 {
		t.Fatalf("target audit created execution state: count %d error %v", count, err)
	}
	for _, gateway := range []TargetOperationGateway{
		component.TargetOperationGateway(workspace, "foreign_kind", target.ID).(TargetOperationGateway),
		component.TargetOperationGateway(workspace, target.ConnectorKind, target.ID+1).(TargetOperationGateway),
		component.TargetOperationGateway(Workspace{}, target.ConnectorKind, target.ID).(TargetOperationGateway),
	} {
		if err := gateway.ConnectorWriteTargetAudit(t.Context(), "connector.fixture_attested", details); err == nil || calls != 1 {
			t.Fatalf("foreign or unavailable target audited: calls %d error %v", calls, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := gateway.ConnectorWriteTargetAudit(ctx, "connector.fixture_attested", details); !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("canceled target audit reached persistence: calls %d error %v", calls, err)
	}
	sentinel := errors.New("audit persistence failed")
	workspace.Targets.TargetAudit = func(context.Context, string, any) error { return sentinel }
	gateway = component.TargetOperationGateway(workspace, target.ConnectorKind, target.ID)
	if err := gateway.ConnectorWriteTargetAudit(t.Context(), "connector.fixture_attested", details); !errors.Is(err, sentinel) {
		t.Fatalf("audit persistence failure hidden: %v", err)
	}
	workspace.Targets.TargetAudit = nil
	gateway = component.TargetOperationGateway(workspace, target.ConnectorKind, target.ID)
	if err := gateway.ConnectorWriteTargetAudit(t.Context(), "connector.fixture_attested", details); !errors.Is(err, ErrRuntimeUnavailable) {
		t.Fatalf("missing audit port silently accepted: %v", err)
	}
}
