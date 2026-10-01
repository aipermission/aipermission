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
	postgresconnector "github.com/aipermission/aipermission/backend/internal/connectors/postgres"
	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolejournal"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

type reconciliationAuditGateway struct {
	connectorapi.TargetOperationGateway
	audit func(context.Context, string, any) error
}

func (gateway *reconciliationAuditGateway) ConnectorWriteTargetAudit(ctx context.Context, action string, payload any) error {
	return gateway.audit(ctx, action, payload)
}

type reconciliationSecrets struct{ reads int }

func (secrets *reconciliationSecrets) GetSecret(context.Context, string) (string, error) {
	secrets.reads++
	return "", errors.New("private-credential-failure")
}

func reconciliationAdapterFixture(t *testing.T) (connectors.RuntimeContext, rolejournal.Entry, *roleHistoryStore, *reconciliationSecrets) {
	t.Helper()
	store := newRoleHistoryStore(t)
	store.readOnly = false
	secrets := &reconciliationSecrets{}
	runtime := connectors.RuntimeContext{
		Target: connectors.TargetView{ID: 1, ConnectorKind: postgresconnector.Kind, Config: map[string]any{"database": "main"}},
		Profile: connectors.CredentialProfileView{ID: 2, TargetID: 1, ConnectorKind: postgresconnector.Kind,
			Kind: "username_password", Public: map[string]any{"username": "admin"}},
		Capabilities: capabilities{rolejournal.CapabilityName: rolejournal.New(store)}, Secrets: secrets,
	}
	anchor, err := rolejournal.Authority(runtime)
	if err != nil {
		t.Fatal(err)
	}
	anchor.ClusterID, anchor.DatabaseOID, anchor.SuccessorOID = "18446744073709551615", 12, 10
	entry, err := rolejournal.New(store).BeginProvision(t.Context(), anchor, "reader")
	if err != nil {
		t.Fatal(err)
	}
	entry.Record.RoleOID = 42
	encoded, err := json.Marshal(entry.Record)
	if err != nil {
		t.Fatal(err)
	}
	store.rows[0].PublicData = string(encoded)
	return runtime, entry, store, secrets
}

func TestRoleReconciliationRejectsInvalidSnapshotsWithoutAuditOrSecrets(t *testing.T) {
	for _, mode := range []string{"unsupported", "missing expected", "extra input", "invalid snapshot", "nil context", "nil gateway", "typed nil gateway", "wrong target", "stale generation", "changed admin"} {
		t.Run(mode, func(t *testing.T) {
			runtime, entry, _, secrets := reconciliationAdapterFixture(t)
			ctx, operation := t.Context(), RoleReconciliationOperation
			input := map[string]any{"expected": entry}
			var gateway connectorapi.TargetOperationGateway = &reconciliationAuditGateway{audit: func(context.Context, string, any) error {
				t.Fatal("invalid request wrote audit")
				return nil
			}}
			want := http.StatusConflict
			switch mode {
			case "unsupported":
				operation, want = "other", http.StatusBadRequest
			case "missing expected":
				input, want = map[string]any{}, http.StatusBadRequest
			case "extra input":
				input["extra"], want = true, http.StatusBadRequest
			case "invalid snapshot":
				input["expected"], want = map[string]any{}, http.StatusBadRequest
			case "nil context":
				ctx = nil
			case "nil gateway":
				gateway = nil
			case "typed nil gateway":
				gateway = (*reconciliationAuditGateway)(nil)
			case "wrong target":
				runtime.Target.ID++
			case "stale generation":
				entry.Record.Generation = entry.Record.Intent.OperationID
				input["expected"] = entry
			case "changed admin":
				runtime.Profile.ID++
			}
			response, err := (adapter{}).RunCredentialTargetOperation(ctx, gateway, runtime, operation, input)
			if err != nil || response.StatusCode != want || secrets.reads != 0 {
				t.Fatalf("invalid request response=%#v err=%v secret reads=%d", response, err, secrets.reads)
			}
		})
	}
}

func TestRoleReconciliationRequiredAuditDeadlineCancellationAndUnknownOutcome(t *testing.T) {
	for _, mode := range []string{"pre audit failed", "pre audit canceled", "dial failed", "post audit failed", "post audit canceled request"} {
		t.Run(mode, func(t *testing.T) {
			runtime, entry, store, secrets := reconciliationAdapterFixture(t)
			before := store.rows[0]
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			gateway := &reconciliationAuditGateway{audit: func(auditCtx context.Context, action string, payload any) error {
				calls++
				deadline, bounded := auditCtx.Deadline()
				if !bounded || time.Until(deadline) <= 0 || time.Until(deadline) > 5*time.Second {
					t.Fatal("required audit lost its five-second deadline")
				}
				data := payload.(map[string]any)
				if data["expected"] != entry || data["admin_profile_id"] != runtime.Profile.ID {
					t.Fatal("audit lost exact current profile or expected snapshot")
				}
				if calls == 1 {
					if action != "connector.role.reconciliation_started" || auditCtx.Err() != nil {
						t.Fatal("pre audit lost action or attached context")
					}
					if mode == "pre audit failed" {
						return errors.New("private-audit-failure")
					}
					if mode == "pre audit canceled" || mode == "post audit canceled request" {
						cancel()
						<-auditCtx.Done()
					}
					if mode == "pre audit canceled" {
						return auditCtx.Err()
					}
					return nil
				}
				if action != "connector.role.reconciliation_finished" || data["outcome"] != "unresolved" || auditCtx.Err() != nil {
					t.Fatal("post audit lost detached unresolved outcome")
				}
				if mode == "post audit failed" {
					return errors.New("private-audit-failure")
				}
				return nil
			}}
			response, err := (adapter{}).RunCredentialTargetOperation(ctx, gateway, runtime, RoleReconciliationOperation, map[string]any{"expected": entry})
			wantCalls, wantSecrets, wantCode := 2, 1, "role_reconciliation_required"
			if strings.HasPrefix(mode, "pre audit") {
				wantCalls, wantSecrets = 1, 0
			} else if mode == "post audit canceled request" {
				wantSecrets = 0
			} else if mode == "post audit failed" {
				wantCode = "role_reconciliation_audit_outcome_unknown"
			}
			if err != nil || response.StatusCode != http.StatusConflict || response.Payload.(map[string]string)["code"] != wantCode ||
				calls != wantCalls || secrets.reads != wantSecrets || store.rows[0] != before || strings.Contains(fmt.Sprint(response.Payload), "private-") {
				t.Fatalf("unsafe unresolved response=%#v err=%v audits=%d secret reads=%d", response, err, calls, secrets.reads)
			}
		})
	}
}
