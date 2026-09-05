package api

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
)

func TestPreparedCredentialUpdateFailsAtomically(t *testing.T) {
	for _, failure := range []string{"stale-secret", "encryption"} {
		t.Run(failure, func(t *testing.T) {
			fixture := newAPITestFixture(t)
			item := createS3IdentityRuntime(t, fixture.server, "http://127.0.0.1:9")
			runtime := fixture.server.activeRuntime()
			store := connectortargets.NewStore(fixture.db)
			target, err := store.GetTarget(t.Context(), item.TargetID)
			if err != nil {
				t.Fatal(err)
			}
			stale, err := store.GetCredentialProfile(t.Context(), target.ID, item.ProfileID)
			if err != nil {
				t.Fatal(err)
			}
			prepared := preparedConnectorCredentialProfileInput{
				Kind: stale.Kind, Label: "must-rollback", Public: stale.Public,
				Secret: map[string]any{"secret_access_key": "fixture-replacement"}, SecretChanged: true,
			}
			if failure == "stale-secret" {
				if err := store.SetCredentialProfileEncryptedSecret(t.Context(), target.ID, stale.ID, stale.EncryptedSecretJSON); err != nil {
					t.Fatal(err)
				}
			} else {
				prepared.Secret["unencodable"] = make(chan struct{})
			}
			before, err := store.GetCredentialProfile(t.Context(), target.ID, stale.ID)
			if err != nil {
				t.Fatal(err)
			}
			var auditBefore int
			if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM audit_outbox`).Scan(&auditBefore); err != nil {
				t.Fatal(err)
			}
			err = fixture.server.withAuditedTransaction(t.Context(), runtime, func(tx *sql.Tx, appendAudit auditAppender) error {
				changed, err := connectortargets.NewTxStore(tx).UpdateTarget(t.Context(), connectortargets.UpdateTargetInput{
					ID: target.ID, ProjectID: target.ProjectID, Name: "must-rollback", Config: target.Config, ExpectedUpdatedAt: target.UpdatedAt,
				})
				if err != nil {
					return err
				}
				if err := appendAudit(tx, "user", nil, 0, "connector.target.updated", map[string]any{"target_id": target.ID}); err != nil {
					return err
				}
				_, err = (connectorTargetHandlers{fixture.server}).updatePreparedCredentialProfile(t.Context(), runtime, tx, changed, stale, prepared)
				return err
			})
			if err == nil {
				t.Fatal("unsafe credential update accepted")
			}
			if failure == "stale-secret" && !errors.Is(err, connectortargets.ErrCredentialProfileUpdateConflict) {
				t.Fatalf("conflict error = %v", err)
			}
			after, err := store.GetCredentialProfile(t.Context(), target.ID, stale.ID)
			if err != nil {
				t.Fatal(err)
			}
			afterTarget, err := store.GetTarget(t.Context(), target.ID)
			if err != nil {
				t.Fatal(err)
			}
			var auditAfter int
			if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM audit_outbox`).Scan(&auditAfter); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(target, afterTarget) || auditBefore != auditAfter {
				t.Fatal("failed profile update did not roll back caller transaction")
			}
		})
	}
}

func TestCredentialEditTransactionBoundaries(t *testing.T) {
	for _, combined := range []bool{false, true} {
		for _, failure := range []string{"", "unchanged-secret", "profile", "surface", "audit"} {
			t.Run(fmt.Sprintf("combined=%t/%s", combined, failure), func(t *testing.T) {
				fixture := newAPITestFixture(t)
				item := createS3IdentityRuntime(t, fixture.server, "http://127.0.0.1:9")
				runtime := fixture.server.activeRuntime()
				store := connectortargets.NewStore(fixture.db)
				target, err := store.GetTarget(t.Context(), item.TargetID)
				if err != nil {
					t.Fatal(err)
				}
				before, err := store.GetCredentialProfile(t.Context(), item.TargetID, item.ProfileID)
				if err != nil {
					t.Fatal(err)
				}
				var auditCount int
				if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM audit_outbox`).Scan(&auditCount); err != nil {
					t.Fatal(err)
				}
				triggers := map[string]string{
					"profile": `CREATE TRIGGER reject_profile_update BEFORE UPDATE ON connector_credential_profiles BEGIN SELECT RAISE(ABORT, 'profile failure'); END`,
					"surface": `CREATE TRIGGER reject_surface_update BEFORE UPDATE ON connector_runtime_surfaces BEGIN SELECT RAISE(ABORT, 'surface failure'); END`,
					"audit":   `CREATE TRIGGER reject_update_audit BEFORE INSERT ON audit_outbox BEGIN SELECT RAISE(ABORT, 'audit failure'); END`,
				}
				if trigger := triggers[failure]; trigger != "" {
					if _, err := fixture.db.Exec(trigger); err != nil {
						t.Fatal(err)
					}
				}
				profileInput := updateConnectorCredentialProfileRequest{
					Kind: before.Kind, Label: "edited", Public: before.Public,
					Secret: map[string]any{"secret_access_key": "replacement-fixture-value"},
				}
				if failure == "unchanged-secret" {
					profileInput.Secret = nil
				}
				url := fmt.Sprintf("/api/connector-targets/%d/profiles/%d", target.ID, before.ID)
				var input any = profileInput
				if combined {
					url = fmt.Sprintf("/api/connector-targets/%d/with-profile/%d", target.ID, before.ID)
					input = updateConnectorTargetWithProfileRequest{
						Target: updateConnectorTargetRequest{Name: "edited-target", Config: target.Config}, Profile: profileInput,
					}
				}
				response := performJSON(fixture.server.Handler(), http.MethodPut, url, "", input)
				after, err := store.GetCredentialProfile(t.Context(), target.ID, before.ID)
				if err != nil {
					t.Fatal(err)
				}
				afterTarget, err := store.GetTarget(t.Context(), target.ID)
				if err != nil {
					t.Fatal(err)
				}
				var afterAuditCount int
				if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM audit_outbox`).Scan(&afterAuditCount); err != nil {
					t.Fatal(err)
				}
				var surfaceLabel string
				if err := fixture.db.QueryRow(`SELECT label FROM connector_runtime_surfaces WHERE id = ?`, item.TransferRuntimeID).Scan(&surfaceLabel); err != nil {
					t.Fatal(err)
				}
				if triggers[failure] != "" {
					if response.Code < 400 {
						t.Fatalf("injected %s failure accepted: %s", failure, response.Body.String())
					}
					if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(target, afterTarget) || afterAuditCount != auditCount || surfaceLabel != before.Label {
						t.Fatalf("failed %s update escaped transaction rollback", failure)
					}
					return
				}
				if response.Code != http.StatusOK {
					t.Fatalf("update: %d %s", response.Code, response.Body.String())
				}
				if after.Label != "edited" || surfaceLabel != "edited" {
					t.Fatal("profile/runtime surface labels diverged")
				}
				wantEvents := 1
				if combined {
					wantEvents = 2
					if afterTarget.Name != "edited-target" {
						t.Fatal("target update missing")
					}
				} else if !reflect.DeepEqual(target, afterTarget) {
					t.Fatal("profile-only edit changed target")
				}
				if afterAuditCount-auditCount != wantEvents {
					t.Fatalf("audit delta = %d, want %d", afterAuditCount-auditCount, wantEvents)
				}
				if failure == "unchanged-secret" {
					if after.EncryptedSecretJSON != before.EncryptedSecretJSON || after.SecretRevision != before.SecretRevision {
						t.Fatal("metadata edit rewrote secret")
					}
					return
				}
				if after.SecretRevision != before.SecretRevision+1 {
					t.Fatal("secret revision did not advance exactly once")
				}
				var secret map[string]any
				if err := recordcrypto.DecryptJSON(runtime.vault, runtime.workspaceUUID, recordcrypto.ConnectorCredentialProfile, after.ID, after.EncryptedSecretJSON, &secret); err != nil {
					t.Fatal(err)
				}
				if secret["secret_access_key"] != "replacement-fixture-value" {
					t.Fatal("replacement secret was not persisted")
				}
			})
		}
	}
}
