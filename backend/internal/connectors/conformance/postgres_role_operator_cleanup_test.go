package conformance_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/apiadapter"
	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolejournal"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	"github.com/jackc/pgx/v5"
)

func TestPostgresRoleOperatorCleanupConfirmsRemoteOrphanBeforeTerminalAudit(t *testing.T) {
	requireConformance(t)
	for _, auditFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "acknowledged", true: "terminal audit lost"}[auditFails], func(t *testing.T) {
			fixture, journal, runtime, entry := newPostgresOperatorFixture(t)
			var err error
			entry, err = journal.ConfirmProvision(t.Context(), entry)
			if err != nil {
				t.Fatal(err)
			}
			// A late-owned fixture object must survive cleanup with the successor
			// as owner. No local credential profile has been published for this role.
			owned := pgx.Identifier{"public", fixture.table + "_owned"}.Sanitize()
			for _, statement := range []string{
				"CREATE TABLE " + owned + " (id integer)",
				"ALTER TABLE " + owned + " OWNER TO " + pgx.Identifier{fixture.role}.Sanitize(),
			} {
				if _, err := fixture.primary.Exec(t.Context(), statement); err != nil {
					t.Fatal(err)
				}
			}
			calls := 0
			gateway := postgresOperatorAudit{audit: func(ctx context.Context, name string, payload any) error {
				calls++
				deadline, bounded := ctx.Deadline()
				if !bounded || ctx.Err() != nil || time.Until(deadline) <= 0 || time.Until(deadline) > 5*time.Second {
					t.Fatal("cleanup audit is not bounded")
				}
				data := payload.(map[string]any)
				fresh, err := journal.Get(ctx, entry.ResourceID)
				if err != nil || data["expected"] != entry || data["admin_profile_id"] != runtime.Profile.ID {
					t.Fatalf("cleanup audit lost snapshot or durable readback: %v", err)
				}
				if calls == 1 {
					if name != "connector.role.cleanup_started" || fresh != entry {
						t.Fatal("cleanup intent audit did not precede remote mutation")
					}
					return nil
				}
				if calls != 2 || name != "connector.role.cleanup_finished" || data["outcome"] != "cleanup_confirmed" ||
					data["confirmed"] != fresh || fresh.Record.Status != rolejournal.Cleaned || fresh.Record.Generation == entry.Record.Generation {
					t.Fatal("cleanup terminal audit preceded durable confirmation")
				}
				assertPostgresOperatorOrphanCleaned(t, fixture, fresh)
				if auditFails {
					return errors.New("fixture cleanup terminal audit failure")
				}
				return nil
			}}
			runner := apiadapter.New().(connectorapi.CredentialTargetOperationRunner)
			response, err := runner.RunCredentialTargetOperation(t.Context(), gateway, runtime, apiadapter.RoleCleanupOperation, map[string]any{
				"expected": entry, "confirmed_role_name": entry.Record.Intent.RoleName,
			})
			fresh, readErr := journal.Get(t.Context(), entry.ResourceID)
			if err != nil || readErr != nil || calls != 2 || fresh.Record.Status != rolejournal.Cleaned || fresh.Reference() != entry.Reference() {
				t.Fatalf("operator cleanup not durable: response=%#v err=%v fresh=%#v read=%v audits=%d", response, err, fresh, readErr, calls)
			}
			if auditFails {
				if response.StatusCode != http.StatusConflict || response.Payload.(map[string]string)["code"] != "role_reconciliation_audit_outcome_unknown" {
					t.Fatalf("lost cleanup audit claimed success: %#v", response)
				}
			} else if response.StatusCode != http.StatusOK || response.Payload.(map[string]any)["entry"] != fresh || response.Payload.(map[string]any)["evidence"] != "acknowledged_remote_cleanup" {
				t.Fatalf("cleanup response lost durable acknowledgement: %#v", response)
			}
			assertPostgresOperatorOrphanCleaned(t, fixture, fresh)
			// Even a lost terminal-audit response must not replay cleanup using
			// the original generation. The durable cleaned decision owns recovery.
			replay, replayErr := runner.RunCredentialTargetOperation(t.Context(), gateway, runtime, apiadapter.RoleCleanupOperation, map[string]any{
				"expected": entry, "confirmed_role_name": entry.Record.Intent.RoleName,
			})
			if replayErr != nil || replay.StatusCode != http.StatusConflict || calls != 2 {
				t.Fatalf("old cleanup snapshot dispatched again: %#v %v audits=%d", replay, replayErr, calls)
			}
		})
	}
}

func assertPostgresOperatorOrphanCleaned(t *testing.T, fixture postgresRoleFenceFixture, entry rolejournal.Entry) {
	t.Helper()
	var remains bool
	if err := fixture.primary.QueryRow(t.Context(), "SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = $1 OR oid = $2)", fixture.role, entry.Record.RoleOID).Scan(&remains); err != nil || remains {
		t.Fatalf("orphan role still exists: %v %v", remains, err)
	}
	var owner uint32
	if err := fixture.primary.QueryRow(t.Context(), "SELECT relowner FROM pg_catalog.pg_class WHERE oid = $1::regclass", pgx.Identifier{"public", fixture.table + "_owned"}.Sanitize()).Scan(&owner); err != nil || owner != entry.Record.Intent.Anchor.SuccessorOID {
		t.Fatalf("late-owned object did not survive with successor owner: %d %v", owner, err)
	}
}
