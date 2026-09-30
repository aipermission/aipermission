package management

import (
	"context"
	"net/http"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/keycleanup"
)

type cleanupOperationGateway struct {
	*cleanupGateway
	audits   []map[string]any
	auditErr error
}

func (gateway *cleanupOperationGateway) ConnectorWriteAudit(_ context.Context, actor string, tokenID *int64, runtimeID int64, action string, payload any) {
	panic("target reconciliation must not use runtime audit authority")
}

func (gateway *cleanupOperationGateway) ConnectorWriteTargetAudit(_ context.Context, action string, payload any) error {
	if gateway.auditErr != nil {
		return gateway.auditErr
	}
	if action != "connector.key_cleanup_attested" {
		panic("unexpected target audit action")
	}
	gateway.audits = append(gateway.audits, payload.(map[string]any))
	return nil
}

func seedCleanupReconciliationIntent(t *testing.T, fixture *cleanupFixture) keycleanup.Entry {
	t.Helper()
	groups, err := planKeyCleanup(t.Context(), fixture.gateway, fixture.runtime, fixture.target, fixture.runtime.profiles)
	if err != nil {
		t.Fatal(err)
	}
	entry, dispatch, err := keycleanup.New(fixture.runtime.journal).Begin(t.Context(), groups[0].Identity)
	if err != nil || !dispatch {
		t.Fatalf("intent fixture: %#v %t %v", entry, dispatch, err)
	}
	return entry
}

func cleanupStatusForTest(t *testing.T, fixture *cleanupFixture, gateway *cleanupOperationGateway) cleanupReconciliationContext {
	t.Helper()
	response, err := (Management{}).RunTargetOperation(t.Context(), gateway, fixture.runtime, fixture.target, cleanupStatusOperation, map[string]any{})
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("inspect cleanup: %#v %v", response, err)
	}
	return response.Payload.(cleanupReconciliationContext)
}

func cleanupAttestForTest(t *testing.T, fixture *cleanupFixture, gateway *cleanupOperationGateway, input any) connectors.ManagementResponse {
	t.Helper()
	response, err := (Management{}).RunTargetOperation(t.Context(), gateway, fixture.runtime, fixture.target, cleanupAttestOperation, input)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func cleanupAttestInputForTest(t *testing.T, snapshot cleanupReconciliationContext) cleanupAttestInput {
	t.Helper()
	if len(snapshot.Records) != 1 || len(snapshot.Records[0].Choices) != 1 {
		t.Fatalf("expected one inspectable fixture record: %#v", snapshot)
	}
	view := snapshot.Records[0]
	input := cleanupAttestInput{
		ResourceID: view.Entry.ResourceID, Generation: view.Entry.Record.Generation,
		ContextDigest: snapshot.ContextDigest, IdentityDigest: view.Choices[0].Digest,
		Reason: "Externally verified exact absence before local reconciliation",
	}
	for _, subject := range view.Choices[0].Subjects {
		input.Coverage = append(input.Coverage, keycleanup.AbsenceEvidence{
			SubjectID: subject.ID, Absent: true, Method: "provider_console",
			Reason: "Fixture operator checked this exact historical location",
		})
	}
	return input
}

func cleanupEntryForTest(t *testing.T, fixture *cleanupFixture, resourceID int64) keycleanup.Entry {
	t.Helper()
	entries, err := keycleanup.New(fixture.runtime.journal).List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.ResourceID == resourceID {
			return entry
		}
	}
	t.Fatalf("cleanup resource %d missing", resourceID)
	return keycleanup.Entry{}
}
