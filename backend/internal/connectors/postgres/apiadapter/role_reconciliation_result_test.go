package apiadapter

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolejournal"
)

func TestRoleReconciliationConfirmedDecisionRequiresBoundedTerminalAudit(t *testing.T) {
	for _, mode := range []string{"confirmed", "audit failed", "request canceled"} {
		t.Run(mode, func(t *testing.T) {
			runtime, expected, _, _ := reconciliationAdapterFixture(t)
			confirmed := expected
			confirmed.Record.Status, confirmed.Record.Generation = rolejournal.Provisioned, strings.Repeat("c", 32)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if mode == "request canceled" {
				cancel()
			}
			calls := 0
			gateway := &reconciliationAuditGateway{audit: func(auditCtx context.Context, action string, payload any) error {
				calls++
				deadline, bounded := auditCtx.Deadline()
				if action != "connector.role.reconciliation_finished" || auditCtx.Err() != nil || !bounded ||
					time.Until(deadline) <= 0 || time.Until(deadline) > 5*time.Second {
					t.Fatal("confirmed outcome audit lost detached bounded ownership")
				}
				data := payload.(map[string]any)
				if data["expected"] != expected || data["confirmed"] != confirmed || data["outcome"] != "presence_confirmed" || data["admin_profile_id"] != runtime.Profile.ID {
					t.Fatal("confirmed decision changed between readback and terminal audit")
				}
				if mode == "audit failed" {
					return errors.New("fixture terminal audit failure")
				}
				return nil
			}}
			decision, _ := decisionForOperation(RoleReconciliationOperation)
			response, err := finishRoleDecision(ctx, gateway, runtime, expected, decision, true, confirmed, nil)
			if err != nil || calls != 1 {
				t.Fatalf("confirmed decision audit=%d err=%v", calls, err)
			}
			if mode == "audit failed" {
				if response.StatusCode != http.StatusConflict || response.Payload.(map[string]string)["code"] != "role_reconciliation_audit_outcome_unknown" {
					t.Fatalf("missing audit falsely confirmed success: %#v", response)
				}
			} else {
				data := response.Payload.(map[string]any)
				if response.StatusCode != http.StatusOK || data["entry"] != confirmed || data["target_id"] != runtime.Target.ID || data["evidence"] != "exact_remote_identity_present" {
					t.Fatalf("confirmed response lost exact decision: %#v", response)
				}
			}
		})
	}
}
