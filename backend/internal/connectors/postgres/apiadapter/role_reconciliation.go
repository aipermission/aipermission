package apiadapter

import (
	"context"
	"net/http"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	postgresconnector "github.com/aipermission/aipermission/backend/internal/connectors/postgres"
	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolejournal"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	resourcecontract "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi/credentialresource"
)

const RoleReconciliationOperation = "role-lifecycle-reconcile"
const RoleCleanupOperation = "role-lifecycle-cleanup"

type roleDecision struct {
	auditPrefix string
	outcome     string
	evidence    string
	run         func(context.Context, connectors.RuntimeContext, rolejournal.Entry, func(context.Context) error) (rolejournal.Entry, error)
}

func decisionForOperation(operation string) (roleDecision, bool) {
	switch operation {
	case RoleReconciliationOperation:
		return roleDecision{"connector.role.reconciliation", "presence_confirmed", "exact_remote_identity_present", postgresconnector.New().ReconcileManagedRole}, true
	case RoleCleanupOperation:
		return roleDecision{"connector.role.cleanup", "cleanup_confirmed", "acknowledged_remote_cleanup", postgresconnector.New().CleanupManagedRole}, true
	default:
		return roleDecision{}, false
	}
}

func (adapter) SupportsCredentialTargetOperation(operation string) bool {
	_, supported := decisionForOperation(operation)
	return supported
}

func (adapter) RunCredentialTargetOperation(ctx context.Context, gateway connectorapi.TargetOperationGateway, runtime connectors.RuntimeContext, operation string, input map[string]any) (connectors.ManagementResponse, error) {
	decision, supported := decisionForOperation(operation)
	wantFields := 1
	if operation == RoleCleanupOperation {
		wantFields = 2
	}
	if !supported || len(input) != wantFields {
		return roleHistoryError(http.StatusBadRequest, "invalid managed Postgres role decision request"), nil
	}
	expected, err := rolejournal.ParseEntry(input["expected"])
	if err != nil {
		return roleHistoryError(http.StatusBadRequest, "invalid managed Postgres role snapshot"), nil
	}
	if operation == RoleCleanupOperation && (input["confirmed_role_name"] != expected.Record.Intent.RoleName || expected.Record.Status != rolejournal.Provisioned) {
		return roleHistoryError(http.StatusBadRequest, "confirm the exact provisioned role name before cleanup"), nil
	}
	if ctx == nil || resourcecontract.IsNilDependency(gateway) || runtime.Target.ConnectorKind != postgresconnector.Kind || expected.Record.Intent.Anchor.TargetID != runtime.Target.ID {
		return roleHistoryError(http.StatusConflict, "managed Postgres reconciliation target is unavailable"), nil
	}
	started := false
	confirmed, err := decision.run(ctx, runtime, expected, func(dialCtx context.Context) error {
		auditCtx, cancel := context.WithTimeout(dialCtx, 5*time.Second)
		defer cancel()
		if err := gateway.ConnectorWriteTargetAudit(auditCtx, decision.auditPrefix+"_started", roleReconciliationAudit(runtime, expected)); err != nil {
			return err
		}
		started = true
		return nil
	})
	return finishRoleDecision(ctx, gateway, runtime, expected, decision, started, confirmed, err)
}

func finishRoleDecision(ctx context.Context, gateway connectorapi.TargetOperationGateway, runtime connectors.RuntimeContext, expected rolejournal.Entry, decision roleDecision, started bool, confirmed rolejournal.Entry, err error) (connectors.ManagementResponse, error) {
	if started {
		auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		payload := roleReconciliationAudit(runtime, expected)
		payload["outcome"] = "unresolved"
		if err == nil {
			payload["outcome"], payload["confirmed"] = decision.outcome, confirmed
		}
		if auditErr := gateway.ConnectorWriteTargetAudit(auditCtx, decision.auditPrefix+"_finished", payload); auditErr != nil {
			return connectors.ManagementResponse{StatusCode: http.StatusConflict, Payload: map[string]string{
				"error": "role decision outcome audit could not be confirmed; reload role history before retrying",
				"code":  "role_reconciliation_audit_outcome_unknown",
			}}, nil
		}
	}
	if err != nil {
		return connectors.ManagementResponse{StatusCode: http.StatusConflict, Payload: map[string]string{
			"error": "role decision could not be confirmed; reload role history and verify the current target and admin profile",
			"code":  "role_reconciliation_required",
		}}, nil
	}
	return connectors.ManagementResponse{StatusCode: http.StatusOK, Payload: map[string]any{
		"target_id": runtime.Target.ID, "entry": confirmed, "evidence": decision.evidence,
	}}, nil
}

func roleReconciliationAudit(runtime connectors.RuntimeContext, expected rolejournal.Entry) map[string]any {
	return map[string]any{"admin_profile_id": runtime.Profile.ID, "expected": expected}
}

var _ connectorapi.CredentialTargetOperationRunner = adapter{}
