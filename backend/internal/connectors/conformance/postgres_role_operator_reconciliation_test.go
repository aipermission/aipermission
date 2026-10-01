package conformance_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/apiadapter"
	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolejournal"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

type postgresOperatorAudit struct {
	connectorapi.TargetOperationGateway
	audit func(context.Context, string, any) error
}

func (gateway postgresOperatorAudit) ConnectorWriteTargetAudit(ctx context.Context, name string, payload any) error {
	return gateway.audit(ctx, name, payload)
}

func newPostgresOperatorFixture(t *testing.T) (postgresRoleFenceFixture, *rolejournal.Journal, connectors.RuntimeContext, rolejournal.Entry) {
	t.Helper()
	fixture := newPostgresRoleFenceFixture(t)
	journal := rolejournal.New(newPostgresReconciliationStore(t))
	runtime := postgresConformanceRuntime(t)
	runtime.Target.ID, runtime.Profile.TargetID, runtime.Profile.ID = 900, 900, 901
	runtime.Target.Ref = "postgres:900:901"
	runtime.Capabilities = postgresOperatorCapabilities{runtime.Capabilities, journal}
	authority, err := rolejournal.Authority(runtime)
	if err != nil {
		t.Fatal(err)
	}
	entry := preparePostgresReconciliation(t, fixture, journal, "provision committed", &authority)
	return fixture, journal, runtime, entry
}

func TestPostgresRoleOperatorReconciliationConfirmsDecisionBeforeTerminalAudit(t *testing.T) {
	requireConformance(t)
	for _, auditFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "acknowledged", true: "terminal audit lost"}[auditFails], func(t *testing.T) {
			fixture, journal, runtime, entry := newPostgresOperatorFixture(t)
			calls := 0
			gateway := postgresOperatorAudit{audit: func(ctx context.Context, name string, payload any) error {
				calls++
				deadline, bounded := ctx.Deadline()
				if !bounded || ctx.Err() != nil || time.Until(deadline) <= 0 || time.Until(deadline) > 5*time.Second {
					t.Fatal("operator audit is not bounded")
				}
				data := payload.(map[string]any)
				if data["admin_profile_id"] != runtime.Profile.ID || data["expected"] != entry {
					t.Fatal("operator audit lost the exact authority/snapshot")
				}
				fresh, err := journal.Get(t.Context(), entry.ResourceID)
				if err != nil {
					t.Fatal(err)
				}
				if calls == 1 {
					if name != "connector.role.reconciliation_started" || fresh != entry {
						t.Fatal("intent audit did not precede confirmation")
					}
					return nil
				}
				if calls != 2 || name != "connector.role.reconciliation_finished" || data["outcome"] != "presence_confirmed" ||
					data["confirmed"] != fresh || fresh.Record.Status != rolejournal.Provisioned || fresh.Record.Generation == entry.Record.Generation {
					t.Fatal("terminal audit did not follow durable confirmed readback")
				}
				if auditFails {
					return errors.New("fixture terminal audit failure")
				}
				return nil
			}}
			runner := apiadapter.New().(connectorapi.CredentialTargetOperationRunner)
			response, err := runner.RunCredentialTargetOperation(t.Context(), gateway, runtime, apiadapter.RoleReconciliationOperation, map[string]any{"expected": entry})
			fresh, readErr := journal.Get(t.Context(), entry.ResourceID)
			if err != nil || readErr != nil || calls != 2 || fresh.Record.Status != rolejournal.Provisioned || fresh.Reference() != entry.Reference() {
				t.Fatalf("operator decision not durable: response=%#v err=%v fresh=%#v read=%v audits=%d", response, err, fresh, readErr, calls)
			}
			if auditFails {
				if response.StatusCode != http.StatusConflict || response.Payload.(map[string]string)["code"] != "role_reconciliation_audit_outcome_unknown" {
					t.Fatalf("lost audit claimed success: %#v", response)
				}
			} else {
				payload := response.Payload.(map[string]any)
				if response.StatusCode != http.StatusOK || payload["target_id"] != runtime.Target.ID || payload["entry"] != fresh || payload["evidence"] != "exact_remote_identity_present" {
					t.Fatalf("confirmed operator response mismatch: %#v", response)
				}
			}
			assertPostgresReconciliationRolePreserved(t, fixture, fresh)
		})
	}
}
