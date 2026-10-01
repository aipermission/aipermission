package apiadapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolejournal"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

func cleanupAdapterFixture(t *testing.T) (connectors.RuntimeContext, rolejournal.Entry, *roleHistoryStore, *reconciliationSecrets) {
	t.Helper()
	runtime, entry, store, secrets := reconciliationAdapterFixture(t)
	entry.Record.Status = rolejournal.Provisioned
	encoded, err := json.Marshal(entry.Record)
	if err != nil {
		t.Fatal(err)
	}
	store.rows[0].PublicData = string(encoded)
	return runtime, entry, store, secrets
}

func TestCredentialRoleDecisionSupportIsExactAndNotAnOrdinaryTargetOperation(t *testing.T) {
	for _, operation := range []string{RoleReconciliationOperation, RoleCleanupOperation, "", "ROLE-LIFECYCLE-CLEANUP", RoleCleanupOperation + " ", RoleHistoryOperation} {
		want := operation == RoleReconciliationOperation || operation == RoleCleanupOperation
		if (adapter{}).SupportsCredentialTargetOperation(operation) != want {
			t.Fatalf("unexpected credential operation support for %q", operation)
		}
		if !want {
			continue
		}
		response, err := (adapter{}).RunTargetOperation(nil, nil, nil, connectorapi.Target{}, operation, nil)
		if err != nil || response.StatusCode != http.StatusBadRequest {
			t.Fatalf("credential decision escaped to ordinary target runtime: %#v %v", response, err)
		}
	}
}

func TestOperatorCleanupRejectsUnconfirmedRoleSnapshotBeforeAuditOrDial(t *testing.T) {
	for _, mode := range []string{"missing name", "wrong name", "wrong type", "extra field", "unresolved", "stale generation", "changed admin", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			runtime, entry, _, secrets := cleanupAdapterFixture(t)
			ctx := t.Context()
			input := map[string]any{"expected": entry, "confirmed_role_name": entry.Record.Intent.RoleName}
			want := http.StatusBadRequest
			switch mode {
			case "missing name":
				delete(input, "confirmed_role_name")
			case "wrong name":
				input["confirmed_role_name"] = "other"
			case "wrong type":
				input["confirmed_role_name"] = true
			case "extra field":
				input["extra"] = true
			case "unresolved":
				entry.Record.Status = rolejournal.CleanupIntent
				input["expected"] = entry
			case "stale generation":
				entry.Record.Generation = entry.Record.Intent.OperationID
				input["expected"], want = entry, http.StatusConflict
			case "changed admin":
				runtime.Profile.ID++
				want = http.StatusConflict
			case "canceled":
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				ctx, want = canceled, http.StatusConflict
			}
			gateway := &reconciliationAuditGateway{audit: func(context.Context, string, any) error { t.Fatal("invalid cleanup wrote audit"); return nil }}
			response, err := (adapter{}).RunCredentialTargetOperation(ctx, gateway, runtime, RoleCleanupOperation, input)
			if err != nil || response.StatusCode != want || secrets.reads != 0 {
				t.Fatalf("invalid cleanup admitted: %#v %v reads=%d", response, err, secrets.reads)
			}
		})
	}
}

func TestOperatorCleanupSharesRequiredAuditAndUncertainOutcomeBoundary(t *testing.T) {
	for _, mode := range []string{"pre audit failed", "pre audit canceled", "dial failed", "post audit failed"} {
		t.Run(mode, func(t *testing.T) {
			runtime, entry, store, secrets := cleanupAdapterFixture(t)
			before := store.rows[0]
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			gateway := &reconciliationAuditGateway{audit: func(auditCtx context.Context, action string, payload any) error {
				calls++
				deadline, bounded := auditCtx.Deadline()
				data := payload.(map[string]any)
				if !bounded || time.Until(deadline) <= 0 || time.Until(deadline) > 5*time.Second || data["expected"] != entry || data["admin_profile_id"] != runtime.Profile.ID {
					t.Fatal("cleanup audit lost deadline or exact operator identity")
				}
				if calls == 1 {
					if action != "connector.role.cleanup_started" || auditCtx.Err() != nil {
						t.Fatal("cleanup pre-audit lost ownership")
					}
					if mode == "pre audit failed" {
						return errors.New("private-audit-failure")
					}
					if mode == "pre audit canceled" {
						cancel()
						return auditCtx.Err()
					}
				} else {
					if action != "connector.role.cleanup_finished" || data["outcome"] != "unresolved" || auditCtx.Err() != nil {
						t.Fatal("cleanup post-audit lost detached outcome")
					}
					if mode == "post audit failed" {
						return errors.New("private-audit-failure")
					}
				}
				return nil
			}}
			response, err := (adapter{}).RunCredentialTargetOperation(ctx, gateway, runtime, RoleCleanupOperation, map[string]any{
				"expected": entry, "confirmed_role_name": entry.Record.Intent.RoleName,
			})
			wantCalls, wantReads, code := 2, 1, "role_reconciliation_required"
			if strings.HasPrefix(mode, "pre audit") {
				wantCalls, wantReads = 1, 0
			} else if mode == "post audit failed" {
				code = "role_reconciliation_audit_outcome_unknown"
			}
			if err != nil || response.StatusCode != http.StatusConflict || response.Payload.(map[string]string)["code"] != code ||
				calls != wantCalls || secrets.reads != wantReads || store.rows[0] != before || strings.Contains(fmt.Sprint(response.Payload), "private-") {
				t.Fatalf("unsafe cleanup response=%#v err=%v audits=%d reads=%d", response, err, calls, secrets.reads)
			}
		})
	}
}

func TestOperatorCleanupAcknowledgementRequiresDetachedTerminalAudit(t *testing.T) {
	for _, failAudit := range []bool{false, true} {
		runtime, expected, _, _ := cleanupAdapterFixture(t)
		confirmed := expected
		confirmed.Record.Status, confirmed.Record.Generation = rolejournal.Cleaned, strings.Repeat("d", 32)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		gateway := &reconciliationAuditGateway{audit: func(auditCtx context.Context, action string, payload any) error {
			data := payload.(map[string]any)
			if auditCtx.Err() != nil || action != "connector.role.cleanup_finished" || data["outcome"] != "cleanup_confirmed" || data["confirmed"] != confirmed {
				t.Fatal("cleanup terminal audit changed durable outcome")
			}
			if failAudit {
				return errors.New("terminal audit failed")
			}
			return nil
		}}
		decision, _ := decisionForOperation(RoleCleanupOperation)
		response, err := finishRoleDecision(ctx, gateway, runtime, expected, decision, true, confirmed, nil)
		if err != nil {
			t.Fatal(err)
		}
		if failAudit {
			if response.StatusCode != http.StatusConflict {
				t.Fatal("cleanup returned success without terminal audit")
			}
		} else if response.StatusCode != http.StatusOK || response.Payload.(map[string]any)["evidence"] != "acknowledged_remote_cleanup" || response.Payload.(map[string]any)["entry"] != confirmed {
			t.Fatalf("cleanup acknowledgement was not preserved: %#v", response)
		}
	}
}
