package actions

import (
	"errors"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func TestNativeWorkflowApprovalDriftCannotDispatch(t *testing.T) {
	for _, field := range []string{"target_config", "profile_public", "permission", "token", "sealed_request"} {
		t.Run(field, func(t *testing.T) {
			fixture := newNativeWorkflowFixture(t, connectortargets.ActionPermissionApprovalRequired, connectors.RiskRead)
			pending, err := fixture.runtime.Call(t.Context(), fixture.call)
			if err != nil || pending.Result.Status != connectors.ResultApprovalPending {
				t.Fatalf("unchanged pending admission: %#v %v", pending, err)
			}
			// Preserve unrelated timestamps: each fault must be detected by the
			// actual authority field, not merely an updated_at comparison.
			query, id := "", pending.Request.ID
			switch field {
			case "target_config":
				query, id = `UPDATE connector_targets SET config_json = '{"drift":true}' WHERE id = ?`, pending.Request.TargetID
			case "profile_public":
				query, id = `UPDATE connector_credential_profiles SET public_json = '{"drift":true}' WHERE id = ?`, pending.Request.ProfileID
			case "permission":
				query, id = `UPDATE token_connector_action_permissions SET execution_rule = 'blocked' WHERE token_id = ?`, fixture.call.TokenID
			case "token":
				query, id = `UPDATE api_tokens SET revoked_at = '2026-10-01T00:00:00Z' WHERE id = ?`, fixture.call.TokenID
			case "sealed_request":
				query = `UPDATE connector_action_requests SET encrypted_payload_json = 'invalid-sealed-record' WHERE id = ?`
			}
			result, err := fixture.database.ExecContext(t.Context(), query, id)
			if err != nil {
				t.Fatal(err)
			}
			if affected, err := result.RowsAffected(); err != nil || affected != 1 {
				t.Fatalf("fault injection missed authority: %d %v", affected, err)
			}
			if _, err := fixture.runtime.RunPending(t.Context(), pending.Request.ID, ""); err == nil {
				t.Fatal("changed authority executed approval")
			}
			stored, err := fixture.store.GetActionRequest(t.Context(), pending.Request.ID)
			if err != nil || stored.Status != connectors.ResultStale || stored.DispatchStartedAt != "" || fixture.connector.dispatches.Load() != 0 {
				t.Fatalf("drift result/finality: %#v %v", stored, err)
			}
			fixture.historyStatus(t, stored.ID, connectors.ResultStale)
			if _, err := fixture.runtime.RunPending(t.Context(), pending.Request.ID, ""); !errors.Is(err, connectortargets.ErrActionRequestNotPending) || fixture.connector.dispatches.Load() != 0 {
				t.Fatalf("stale approval redispatched: %v", err)
			}
		})
	}
}

func TestNativeWorkflowTokenReaderPreservesMissingAndActiveContracts(t *testing.T) {
	fixture := newNativeWorkflowFixture(t, connectortargets.ActionPermissionApprovalRequired, connectors.RiskRead)
	token, err := fixture.runtime.tokens.Get(t.Context(), fixture.call.TokenID, time.Now())
	if err != nil || token.ID != fixture.call.TokenID || !token.Active {
		t.Fatalf("native active token: %#v %v", token, err)
	}
	if token, err := fixture.runtime.tokens.Get(t.Context(), fixture.call.TokenID+1, time.Now()); !errors.Is(err, ErrTokenNotFound) || token.ID != 0 || token.Active {
		t.Fatalf("native missing token granted authority or lost sentinel: %#v %v", token, err)
	}
}
