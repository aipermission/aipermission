package gatewayconnectoractions

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/messagequeue"
)

func TestApprovalAuthorityChangesNeverDispatchConnector(t *testing.T) {
	for _, test := range []struct {
		name   string
		drift  string
		change func(*testing.T, approvalFixture)
	}{
		{name: "unchanged positive control"},
		{name: "permission blocked", drift: "permission", change: func(t *testing.T, fixture approvalFixture) {
			fixture.permission(t, connectortargets.ActionPermissionBlocked)
		}},
		{name: "token revoked", drift: "token", change: func(t *testing.T, fixture approvalFixture) {
			if _, err := fixture.workspace.Storage.Tokens.Revoke(t.Context(), fixture.tokenID); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "profile public scope changed", drift: "profile", change: func(t *testing.T, fixture approvalFixture) {
			_, err := fixture.store.UpdateCredentialProfile(t.Context(), connectortargets.UpdateCredentialProfileInput{
				TargetID: fixture.profile.TargetID, ProfileID: fixture.profile.ID, ConnectorKind: "fixture",
				Kind: "test", Label: "default", Public: map[string]any{"scope": "changed"},
			})
			if err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newApprovalFixture(t)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			pending, err := fixture.component.Call(ctx, fixture.workspace, fixture.call)
			if err != nil || pending.Request.ID < 1 || pending.Request.Status != connectors.ResultApprovalPending {
				t.Fatalf("pending approval = %#v, %v", pending, err)
			}
			if fixture.connector.dispatches.Load() != 0 || pending.Request.EncryptedPayloadJSON == "" ||
				strings.Contains(pending.Request.EncryptedPayloadJSON, "approval-payload") {
				t.Fatal("pending request was dispatched or was not sealed")
			}
			if test.change != nil {
				test.change(t, fixture)
			}
			approval, err := fixture.component.Approval(fixture.workspace)
			if err != nil {
				t.Fatal(err)
			}
			result, runErr := approval.RunPending(ctx, pending.Request.ID, "Approve the unchanged fixture")
			stored, err := fixture.store.GetActionRequest(ctx, pending.Request.ID)
			if err != nil {
				t.Fatal(err)
			}
			messages, err := messagequeue.NewStore(fixture.workspace.Storage.Database, nil).List(ctx, messagequeue.Filter{TokenID: fixture.tokenID})
			if err != nil {
				t.Fatal(err)
			}
			if test.drift == "" {
				output, ok := stored.Output.(map[string]any)
				if runErr != nil || result.ID != pending.Request.ID || result.Status != connectors.ResultCompleted ||
					stored.Status != connectors.ResultCompleted || !ok || output["value"] != "approval-payload" ||
					fixture.connector.dispatches.Load() != 1 || stored.DispatchStartedAt == "" || len(messages) != 1 {
					t.Fatalf("unchanged approval did not dispatch exactly once: result=%#v stored=%#v err=%v dispatches=%d messages=%d",
						result, stored, runErr, fixture.connector.dispatches.Load(), len(messages))
				}
				return
			}
			if runErr == nil || stored.Status != connectors.ResultStale || stored.ApprovalContextDrift != test.drift ||
				stored.CompletedAt == nil || stored.DispatchStartedAt != "" || fixture.connector.dispatches.Load() != 0 || len(messages) != 0 {
				t.Fatalf("changed approval crossed dispatch boundary: stored=%#v err=%v dispatches=%d messages=%d",
					stored, runErr, fixture.connector.dispatches.Load(), len(messages))
			}
			if _, err := approval.RunPending(ctx, pending.Request.ID, "Retry stale approval"); !errors.Is(err, connectortargets.ErrActionRequestNotPending) || fixture.connector.dispatches.Load() != 0 {
				t.Fatalf("stale approval was reusable: err=%v dispatches=%d", err, fixture.connector.dispatches.Load())
			}
		})
	}
}
