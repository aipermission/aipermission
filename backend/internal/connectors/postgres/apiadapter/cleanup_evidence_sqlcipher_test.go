package apiadapter

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectormanagement"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolejournal"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	appdb "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/observability"
)

func TestRecoveryDrillPostgresSQLCipherTerminalCleanupRetiresWithoutOriginalAdmin(t *testing.T) {
	for _, scenario := range []struct {
		status rolejournal.Status
		admin  string
	}{
		{rolejournal.Cleaned, "archived"},
		{rolejournal.Cleaned, "rotated"},
		{rolejournal.Cleaned, "renamed"},
		{rolejournal.Provisioned, "archived"},
		{rolejournal.CleanupIntent, "archived"},
	} {
		t.Run(string(scenario.status)+"/"+scenario.admin, func(t *testing.T) {
			fixture := newCleanupEvidenceSQLCipherFixture(t, scenario.status)
			changeCleanupEvidenceAdmin(t, fixture, scenario.admin)
			if err := fixture.database.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := appdb.OpenEncrypted(fixture.path, cleanupEvidencePassword)
			if err != nil {
				t.Fatal(err)
			}
			fixture.database = reopened
			fixture.bindResources(t)
			held, cleaned, hooks := false, false, 0
			coordinator := observability.NewCoordinator(reopened, nil, nil, nil)
			runtime := connectormanagement.CredentialRuntimePorts{
				DecryptSecret: func(context.Context, int64, string) (map[string]any, error) {
					t.Fatal("terminal/unavailable-admin cleanup decrypted a credential")
					return nil, nil
				},
				RuntimeContext: func(connectortargets.Target, connectortargets.CredentialProfile, map[string]any, connectormanagement.CredentialBoundary) connectors.RuntimeContext {
					t.Fatal("terminal/unavailable-admin cleanup acquired execution authority")
					return connectors.RuntimeContext{}
				},
				RedactResult: func(_ context.Context, result connectors.ActionResult, _ connectormanagement.CredentialBoundary) (connectors.ActionResult, error) {
					return result, nil
				},
				RedactText: func(_ context.Context, value string) string { return value },
			}
			scope := connectormanagement.ProfileDeletionScope{
				Database: reopened,
				AcquireExclusive: func(context.Context) (func(), error) {
					held = true
					return func() { held = false }, nil
				},
				Cleanup: func(ctx context.Context, target connectortargets.Target, profile connectortargets.CredentialProfile) (connectormanagement.ProfileCleanupOutcome, error) {
					if !held || profile.ID != fixture.managed.ID || target.ID != fixture.target.ID {
						t.Fatal("cleanup ran outside exclusive fresh target/profile admission")
					}
					outcome, err := connectormanagement.CleanupProvisionedCredentialProfileIfNeeded(ctx, connectormanagement.ManagedCredentialCleanupScope{
						Database: reopened, Registry: fixture.registry, Runtime: runtime,
						EvidenceCapabilities: func(string) (connectors.RuntimeCapabilityResolver, error) {
							if !held {
								t.Fatal("evidence capability constructed outside lifecycle admission")
							}
							return capabilities((adapter{}).EvidenceCapabilities(testEvidenceResources{fixture.resources})), nil
						},
					}, target, profile)
					cleaned = err == nil && outcome.Required
					return outcome, err
				},
				BeforeDelete: func(context.Context, connectortargets.Target, connectortargets.CredentialProfile) error {
					if !held || !cleaned {
						t.Fatal("profile retirement preceded completed evidence")
					}
					hooks++
					return nil
				},
				WithTransaction: func(ctx context.Context, mutate func(*sql.Tx, connectormanagement.AuditAppender) error) error {
					if !held || !cleaned {
						t.Fatal("profile/audit transaction ran outside exclusive cleanup")
					}
					return coordinator.WithTransaction(ctx, func(tx *sql.Tx, appendAudit observability.Appender) error {
						return mutate(tx, connectormanagement.AuditAppender(appendAudit))
					})
				},
				AfterLifecycleChange: func(context.Context, connectormanagement.TargetLifecycleChange) error {
					if !held {
						t.Fatal("retirement invalidation ran outside exclusive admission")
					}
					return nil
				},
			}
			handler := connectormanagement.NewProfileDeletionHTTPHandler(func(http.ResponseWriter) (connectormanagement.ProfileDeletionScope, bool) { return scope, true })
			request := httptest.NewRequest(http.MethodDelete, "/", nil)
			request.SetPathValue("id", strconv.FormatInt(fixture.target.ID, 10))
			request.SetPathValue("profile_id", strconv.FormatInt(fixture.managed.ID, 10))
			response := httptest.NewRecorder()
			handler.Delete(response, request)
			assertCleanupEvidenceRetirement(t, fixture, scenario.status, response, held, hooks)
		})
	}
}

func assertCleanupEvidenceRetirement(t *testing.T, fixture *cleanupEvidenceSQLCipherFixture, status rolejournal.Status, response *httptest.ResponseRecorder, held bool, hooks int) {
	t.Helper()
	var audits int
	if err := fixture.database.QueryRow(`SELECT COUNT(*) FROM audit_outbox WHERE action = 'connector.profile.deleted'`).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	_, lookupErr := connectortargets.NewStore(fixture.database).GetCredentialProfile(t.Context(), fixture.target.ID, fixture.managed.ID)
	if held {
		t.Fatal("cleanup evidence left lifecycle admission held")
	}
	if status != rolejournal.Cleaned {
		if response.Code == http.StatusNoContent || lookupErr != nil || audits != 0 || hooks != 0 {
			t.Fatalf("unresolved cleanup retired local profile: status=%d lookup=%v audits=%d hooks=%d", response.Code, lookupErr, audits, hooks)
		}
		return
	}
	if response.Code != http.StatusNoContent || !errors.Is(lookupErr, connectortargets.ErrTargetProfileNotFound) || audits != 1 || hooks != 1 {
		t.Fatalf("terminal retirement failed: status=%d body=%s lookup=%v audits=%d hooks=%d", response.Code, response.Body.String(), lookupErr, audits, hooks)
	}
	var payload string
	if err := fixture.database.QueryRow(`SELECT payload_json FROM audit_outbox WHERE action = 'connector.profile.deleted'`).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var audit struct {
		ExternalCleanup struct {
			Status string `json:"status"`
			Output struct {
				PreviouslyConfirmed bool `json:"previously_confirmed"`
			} `json:"output"`
		} `json:"external_cleanup"`
	}
	if err := json.Unmarshal([]byte(payload), &audit); err != nil || audit.ExternalCleanup.Status != "completed" || !audit.ExternalCleanup.Output.PreviouslyConfirmed {
		t.Fatalf("terminal retirement audit lost acknowledgement: %s %v", payload, err)
	}
}
