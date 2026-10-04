package gatewayinfrastructure

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeInfrastructureAuditFailureRollsBackSettings(t *testing.T) {
	component := NewComponent(filepath.Join(t.TempDir(), "selection.aipdb"), nil)
	handle := openNativeInfrastructureHandle(t, component, "audit-fixture")
	access := component.AccessOwner()
	initial, err := access.ReadSecuritySettings(t.Context(), handle)
	if err != nil {
		t.Fatal(err)
	}
	before := infrastructureOutboxCount(t, handle)
	database := handle.workspace.WorkspaceDatabase()
	if _, err := database.ExecContext(t.Context(), `CREATE TRIGGER reject_infrastructure_audit BEFORE INSERT ON audit_outbox
		BEGIN SELECT RAISE(ABORT, 'injected infrastructure audit failure'); END`); err != nil {
		t.Fatal(err)
	}
	want := initial
	want.ReusableTokens = !initial.ReusableTokens
	if _, err := access.UpdateSecuritySettings(t.Context(), handle, want); err == nil || !strings.Contains(err.Error(), "injected infrastructure audit failure") {
		t.Fatalf("required audit failure was not observed: %v", err)
	}
	if got, err := access.ReadSecuritySettings(t.Context(), handle); err != nil || got != initial || infrastructureOutboxCount(t, handle) != before {
		t.Fatalf("audit failure did not roll back: settings=%#v error=%v", got, err)
	}
	if got := infrastructureDurableSettings(t, handle); got != initial {
		t.Fatalf("durable settings escaped rollback despite cached view: %#v", got)
	}
	if _, err := database.ExecContext(t.Context(), `DROP TRIGGER reject_infrastructure_audit`); err != nil {
		t.Fatal(err)
	}
	if got, err := access.UpdateSecuritySettings(t.Context(), handle, want); err != nil || got != want {
		t.Fatalf("healthy positive control: settings=%#v error=%v", got, err)
	}
	if infrastructureOutboxCount(t, handle) != before+1 {
		t.Fatal("healthy mutation did not append exactly one audit event")
	}
	if got := infrastructureDurableSettings(t, handle); got != want {
		t.Fatalf("healthy mutation updated cache without durable commit: %#v", got)
	}
	if _, err := access.UpdateSecuritySettings(t.Context(), nil, initial); !errors.Is(err, ErrWorkspaceHandleUnavailable) {
		t.Fatalf("missing handle reached mutation: %v", err)
	}
	if got, err := access.ReadSecuritySettings(t.Context(), handle); err != nil || got != want || infrastructureOutboxCount(t, handle) != before+1 {
		t.Fatalf("missing handle disturbed native storage: %#v %v", got, err)
	}
}
