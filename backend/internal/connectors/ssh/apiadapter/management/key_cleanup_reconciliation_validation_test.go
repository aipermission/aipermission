package management

import (
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	resourcecontract "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi/credentialresource"
)

func TestCleanupReconciliationSnapshotFailureIsSanitizedAndReadOnly(t *testing.T) {
	const canary = "private-fixture-error-must-not-reach-response"
	for _, failure := range []string{"profiles", "journal"} {
		t.Run(failure, func(t *testing.T) {
			fixture := newCleanupFixture(t)
			server := startCleanupSSHServer(t, fixture, "operator")
			gateway := &cleanupOperationGateway{cleanupGateway: fixture.gateway}
			original := seedCleanupReconciliationIntent(t, fixture)
			if failure == "profiles" {
				fixture.runtime.profilesErr = errors.New(canary)
			} else {
				if _, err := fixture.runtime.journal.Update(t.Context(), original.ResourceID, resourcecontract.UpdateCredentialResourceInput{Name: "Malformed fixture evidence", PublicData: "{}"}); err != nil {
					t.Fatal(err)
				}
			}
			response, err := (Management{}).RunTargetOperation(t.Context(), gateway, fixture.runtime, fixture.target, cleanupStatusOperation, map[string]any{})
			if err != nil || response.StatusCode != http.StatusConflict || strings.Contains(response.Payload.(map[string]string)["error"], canary) || len(gateway.audits) != 0 {
				t.Fatalf("unobservable evidence leaked details or success: %#v %v", response, err)
			}
			if fixture.runtime.keys.secretReads != 0 || fixture.runtime.journal.secretReads != 0 || server.authAttempts.Load() != 0 || server.commands.Load() != 0 {
				t.Fatal("failed evidence snapshot used private credentials or remote execution")
			}
		})
	}
}

func TestCleanupReconciliationRejectsIncompleteOrForeignEvidence(t *testing.T) {
	changes := map[string]func(*cleanupAttestInput){
		"resource":             func(input *cleanupAttestInput) { input.ResourceID++ },
		"generation":           func(input *cleanupAttestInput) { input.Generation = "not-current" },
		"context":              func(input *cleanupAttestInput) { input.ContextDigest = "not-current" },
		"identity":             func(input *cleanupAttestInput) { input.IdentityDigest = "not-current" },
		"missing_coverage":     func(input *cleanupAttestInput) { input.Coverage = nil },
		"foreign_subject":      func(input *cleanupAttestInput) { input.Coverage[0].SubjectID = "foreign" },
		"unchecked_absence":    func(input *cleanupAttestInput) { input.Coverage[0].Absent = false },
		"local_method":         func(input *cleanupAttestInput) { input.Coverage[0].Method = "same_credential" },
		"empty_subject_reason": func(input *cleanupAttestInput) { input.Coverage[0].Reason = " " },
		"empty_reason":         func(input *cleanupAttestInput) { input.Reason = " " },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			fixture := newCleanupFixture(t)
			server := startCleanupSSHServer(t, fixture, "operator")
			gateway := &cleanupOperationGateway{cleanupGateway: fixture.gateway}
			original := seedCleanupReconciliationIntent(t, fixture)
			input := cleanupAttestInputForTest(t, cleanupStatusForTest(t, fixture, gateway))
			change(&input)
			response := cleanupAttestForTest(t, fixture, gateway, input)
			entry := cleanupEntryForTest(t, fixture, original.ResourceID)
			if response.StatusCode != http.StatusConflict || !reflect.DeepEqual(entry, original) || len(gateway.audits) != 0 {
				t.Fatalf("invalid evidence accepted: %#v entry %#v", response, entry)
			}
			if fixture.runtime.keys.secretReads != 0 || fixture.runtime.journal.secretReads != 0 || server.authAttempts.Load() != 0 || server.commands.Load() != 0 {
				t.Fatal("invalid evidence delivered credentials or dispatched")
			}
		})
	}
}

func TestCleanupReconciliationStrictInputPrecedesSnapshot(t *testing.T) {
	for _, operation := range []string{cleanupStatusOperation, cleanupAttestOperation} {
		for _, value := range []any{nil, map[string]any(nil), map[string]any{"unknown": true}, []any{}, "invalid", make(chan int)} {
			response, err := (Management{}).RunTargetOperation(t.Context(), &cleanupOperationGateway{}, nil, newCleanupFixture(t).target, operation, value)
			if err != nil || response.StatusCode != http.StatusBadRequest {
				t.Fatalf("malformed input inspected runtime: operation %s response %#v error %v", operation, response, err)
			}
		}
	}
}
