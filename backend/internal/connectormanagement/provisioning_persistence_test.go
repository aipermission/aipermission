package connectormanagement

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/transactionstate"
)

func TestProfilePublicationReconciliationRequiresCompletedPreparation(t *testing.T) {
	for _, publication := range []profilePublication{
		{}, {profile: connectortargets.CredentialProfile{ID: 9}}, {prepared: true},
	} {
		cleanupCount, audits := 0, 0
		scope := ProvisioningScope{Runtime: managementCredentialRuntimePorts(), AuditRequired: func(context.Context, string, any) error {
			audits++
			return nil
		}}
		response := httptest.NewRecorder()
		handler := NewProvisioningHTTPHandler(nil)
		handler.failProfilePublication(t.Context(), response, scope, publicationCleanupConnector{cleanupCount: &cleanupCount},
			connectortargets.Target{}, connectortargets.CredentialProfile{}, nil, connectors.ProvisionedCredentialProfile{}, publication,
			transactionstate.UnknownWithSafeReadback(errors.New("acknowledgement lost")))
		if response.Code != http.StatusConflict || cleanupCount != 0 || audits != 1 {
			t.Fatalf("incomplete publication was reconciled without evidence: %d cleanup=%d audits=%d", response.Code, cleanupCount, audits)
		}
	}
}

func TestProvisioningHandlerReconcilesAmbiguousLocalTransactionWithoutRemoteDeletion(t *testing.T) {
	for _, test := range []struct {
		name      string
		commit    bool
		confirmed bool
		change    bool
		auditFail bool
		unsafe    bool
		status    int
		cleanup   int
		code      string
	}{
		{name: "committed lost acknowledgement", commit: true, status: http.StatusCreated},
		{name: "unretired owner connection", commit: true, unsafe: true, status: http.StatusConflict, code: "profile_persistence_outcome_unknown"},
		{name: "confirmed rollback", confirmed: true, status: http.StatusInternalServerError, cleanup: 1},
		{name: "unconfirmed absent row", status: http.StatusConflict, code: "profile_persistence_outcome_unknown"},
		{name: "committed row changed", commit: true, change: true, status: http.StatusConflict, code: "profile_persistence_outcome_unknown"},
		{name: "uncertain audit failed", auditFail: true, status: http.StatusInternalServerError, code: "provisioning_reconciliation_audit_failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newManagementHTTPFixture(t)
			cleanupCount := 0
			registry := connectors.NewRegistry()
			if err := registry.Register(publicationCleanupConnector{cleanupCount: &cleanupCount}); err != nil {
				t.Fatal(err)
			}
			state := &provisioningHTTPTestState{registry: registry}
			if test.auditFail {
				state.auditErr = errors.New("audit failed")
			}
			cause := errors.New("local finalization lost acknowledgement managed-secret")
			state.finishTransaction = func(tx *sql.Tx) error {
				var err error
				if test.commit {
					err = tx.Commit()
				} else {
					err = tx.Rollback()
				}
				if err != nil {
					t.Fatal(err)
				}
				if test.change {
					if _, err := fixture.database.Exec(`UPDATE connector_credential_profiles SET label = 'changed' WHERE id = ?`, state.ensuredProfileID); err != nil {
						t.Fatal(err)
					}
				}
				if test.confirmed {
					return transactionstate.NotCommitted(cause)
				}
				if test.unsafe {
					return transactionstate.Unknown(cause)
				}
				return transactionstate.UnknownWithSafeReadback(cause)
			}
			response := performProvisioningRequest(t, fixture, state)
			if response.Code != test.status || cleanupCount != test.cleanup {
				t.Fatalf("status=%d cleanup=%d, want %d/%d: %s", response.Code, cleanupCount, test.status, test.cleanup, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "managed-secret") || (test.code != "" && !strings.Contains(response.Body.String(), test.code)) {
				t.Fatalf("unsafe or unclassified response: %s", response.Body.String())
			}
			if test.status == http.StatusCreated {
				var result ProvisionResponse
				if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Profile.ID != state.ensuredProfileID {
					t.Fatalf("reconciled response did not identify persisted profile: %v %#v", err, result)
				}
			}
			if !test.commit && !test.confirmed && !strings.Contains(strings.Join(state.auditActions, ","), "provisioning_reconciliation_required") {
				t.Fatalf("uncertain publication lost required audit: %v", state.auditActions)
			}
		})
	}
}
