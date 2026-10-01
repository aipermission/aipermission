package connectormanagement

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectorcredentials"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func TestCompletedCredentialCleanupEvidencePrecedesAdminLookupAndDecryption(t *testing.T) {
	scope, target, profile := cleanupEvidenceFixture(t)
	reads, projections := 0, 0
	connector := cleanupEvidenceConnector{read: func(ctx context.Context, evidence connectors.CleanupEvidenceContext, got connectors.CredentialProfileView) (*connectors.ActionResult, error) {
		reads++
		if ctx != t.Context() || !reflect.DeepEqual(evidence.Target, connectorcredentials.TargetView(target, profile.ID)) ||
			!reflect.DeepEqual(got, connectortargets.CredentialProfileView(profile)) || evidence.Capabilities == nil {
			t.Fatal("local evidence reader lost the current public target/profile identity")
		}
		evidence.Target.Config["endpoint"] = "must-not-mutate-core"
		got.Public["managed_marker"] = "must-not-mutate-core"
		return &connectors.ActionResult{Status: connectors.ResultCompleted, Output: "unprojected"}, nil
	}}
	scope.Runtime.RedactResult = func(ctx context.Context, result connectors.ActionResult, boundary CredentialBoundary) (connectors.ActionResult, error) {
		projections++
		if ctx != t.Context() || result.Output != "unprojected" || !boundary.Valid() {
			t.Fatal("local confirmation skipped the result boundary")
		}
		result.Output = "projected"
		return result, nil
	}
	registry := connectors.NewRegistry()
	if err := registry.Register(connector); err != nil {
		t.Fatal(err)
	}
	scope.Registry = registry
	// An unopened database cannot service any lookup. Confirmed evidence must
	// return before attempting to query the original administrator.
	scope.Database = &sql.DB{}
	outcome, err := CleanupProvisionedCredentialProfileIfNeeded(t.Context(), scope, target, profile)
	if err != nil || !outcome.Required || outcome.Status != string(connectors.ResultCompleted) || outcome.Output != "projected" || reads != 1 || projections != 1 {
		t.Fatalf("terminal outcome=%#v error=%v reads=%d projections=%d", outcome, err, reads, projections)
	}
	if target.Config["endpoint"] != "current-endpoint" || profile.Public["managed_marker"] != "generated" {
		t.Fatal("connector modified the core's public identity snapshot")
	}
}

func TestCompletedCredentialCleanupEvidenceAllowsOnlyNilFallback(t *testing.T) {
	for _, status := range []connectors.ResultStatus{connectors.ResultRunning, connectors.ResultFailed, connectors.ResultOutcomeUnknown, ""} {
		t.Run(string(status), func(t *testing.T) {
			scope, target, profile := cleanupEvidenceFixture(t)
			connector := cleanupEvidenceConnector{read: func(context.Context, connectors.CleanupEvidenceContext, connectors.CredentialProfileView) (*connectors.ActionResult, error) {
				return &connectors.ActionResult{Status: status}, nil
			}}
			if outcome, handled, err := completedCredentialCleanupEvidence(t.Context(), scope, connector, target, profile); !handled || err == nil || outcome.Required {
				t.Fatalf("nonterminal evidence fallback/success: %#v %t %v", outcome, handled, err)
			}
		})
	}
	scope, target, profile := cleanupEvidenceFixture(t)
	connector := cleanupEvidenceConnector{read: func(context.Context, connectors.CleanupEvidenceContext, connectors.CredentialProfileView) (*connectors.ActionResult, error) {
		return nil, nil
	}}
	if outcome, handled, err := completedCredentialCleanupEvidence(t.Context(), scope, connector, target, profile); handled || err != nil || outcome.Required {
		t.Fatalf("nil result must use the normal authenticated cleanup: %#v %t %v", outcome, handled, err)
	}
	scope.EvidenceCapabilities = func(string) (connectors.RuntimeCapabilityResolver, error) {
		t.Fatal("connector without the optional contract requested evidence capabilities")
		return nil, nil
	}
	if _, handled, err := completedCredentialCleanupEvidence(t.Context(), scope, managementTestConnector{}, target, profile); handled || err != nil {
		t.Fatalf("ordinary connector lost normal cleanup path: %t %v", handled, err)
	}
}

func TestCompletedCredentialCleanupEvidenceRejectsErrorAndFollowupHandles(t *testing.T) {
	for _, result := range []connectors.ActionResult{
		{Status: connectors.ResultCompleted, Error: "not confirmed"},
		{Status: connectors.ResultCompleted, Handles: connectors.ActionHandles{RequestID: 8}},
		{Status: connectors.ResultCompleted, Handles: connectors.ActionHandles{SessionID: 8}},
		{Status: connectors.ResultCompleted, Handles: connectors.ActionHandles{BatchID: 8}},
		{Status: connectors.ResultCompleted, Handles: connectors.ActionHandles{FollowupTool: "poll"}},
	} {
		scope, target, profile := cleanupEvidenceFixture(t)
		connector := cleanupEvidenceConnector{read: func(context.Context, connectors.CleanupEvidenceContext, connectors.CredentialProfileView) (*connectors.ActionResult, error) {
			return &result, nil
		}}
		if outcome, handled, err := completedCredentialCleanupEvidence(t.Context(), scope, connector, target, profile); !handled || err == nil || outcome.Required {
			t.Fatalf("ambiguous result accepted: %#v %t %v", outcome, handled, err)
		}
	}
}

func TestCompletedCredentialCleanupEvidenceRedactsErrorsAndFailsClosedOnProjection(t *testing.T) {
	for _, where := range []string{"capability", "reader", "projection"} {
		t.Run(where, func(t *testing.T) {
			scope, target, profile := cleanupEvidenceFixture(t)
			failure := errors.New("private-resource-diagnostic")
			scope.Runtime.RedactText = func(context.Context, string) string { return "safe diagnostic" }
			connector := cleanupEvidenceConnector{read: func(context.Context, connectors.CleanupEvidenceContext, connectors.CredentialProfileView) (*connectors.ActionResult, error) {
				if where == "reader" {
					return &connectors.ActionResult{Status: connectors.ResultCompleted}, failure
				}
				return &connectors.ActionResult{Status: connectors.ResultCompleted}, nil
			}}
			if where == "capability" {
				scope.EvidenceCapabilities = func(string) (connectors.RuntimeCapabilityResolver, error) { return nil, failure }
			} else if where == "projection" {
				scope.Runtime.RedactResult = func(context.Context, connectors.ActionResult, CredentialBoundary) (connectors.ActionResult, error) {
					return connectors.ActionResult{}, failure
				}
			}
			outcome, handled, err := completedCredentialCleanupEvidence(t.Context(), scope, connector, target, profile)
			if !handled || err == nil || outcome.Required || (where != "projection" && err.Error() != "safe diagnostic") {
				t.Fatalf("failed evidence leaked/fell back: %#v %t %v", outcome, handled, err)
			}
		})
	}
}
